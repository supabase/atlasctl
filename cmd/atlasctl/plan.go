package main

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// selectAll builds a filtered probe pool from snap and runs per-measurement
// selection, returning results keyed by measurement name. The orderer is
// shared across all measurements so that cohorts with identical CohortCfg
// benefit from the in-memory ordering cache.
func selectAll(ctx context.Context, snap snapshot.Snapshot, cfg config.Config) (map[string][]selection.SelectedCohort, error) {
	probes := selection.NewProbes(len(snap.Probes))
	for _, p := range snap.Probes {
		if !selection.HardExcluded(p, cfg.ExcludeTags) {
			probes.Append(p)
		}
	}
	probes.Close()

	orderer := selection.NewDefaultOrderer(cfg.GeoDiversity.H3Resolution)
	allSelected := make(map[string][]selection.SelectedCohort, len(cfg.Measurements))
	for _, msm := range cfg.Measurements {
		selected, err := selection.Select(ctx, probes, msm.Cohorts, orderer, cfg.GeoDiversity.H3Resolution)
		if err != nil {
			return nil, err
		}
		for i := range selected {
			selected[i].Measurement = msm.Name
		}
		allSelected[msm.Name] = selected
	}
	return allSelected, nil
}

func newPlanCmd(f *globalFlags, deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Show the diff between desired and live measurement state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cfg, err := config.Load(f.ConfigPath)
			if err != nil {
				return err
			}
			if len(cfg.Measurements) == 0 {
				return errors.New("no measurements defined in config: add at least one measurement before running plan")
			}

			snap, err := snapshot.Load(f.SnapshotFile)
			if err != nil {
				return err
			}

			state, err := plan.LoadState(f.StateFile)
			if err != nil {
				if !errors.Is(err, plan.ErrStateNotFound) {
					return err
				}
				state = plan.NewStateFile()
			}

			allSelected, err := selectAll(ctx, snap, *cfg)
			if err != nil {
				return err
			}
			desired := plan.DesiredState(*cfg, allSelected)

			apiKey, err := resolveAPIKey(f.APIKey)
			if err != nil {
				return err
			}
			msmClient, err := deps.NewMsmClient(apiKey, f.Verbose, plan.NewTagCodec(cfg.TagPrefix))
			if err != nil {
				return err
			}

			cs, warnings, err := plan.LiveDiff(ctx, desired, state, msmClient)
			if err != nil {
				return err
			}

			printChangeset(cmd.OutOrStdout(), cs)
			printWarnings(cmd.OutOrStdout(), warnings)
			printCreditEstimate(cmd.OutOrStdout(), plan.EstimateCredits(desired))
			return nil
		},
	}
}
