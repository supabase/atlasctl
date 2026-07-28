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

// selectAll builds a filtered probe pool and runs per-measurement selection,
// returning results keyed by measurement name. The orderer is shared across
// all measurements so that cohorts with identical CohortCfg benefit from the
// in-memory ordering cache.
func selectAll(ctx context.Context, probeList []snapshot.Probe, cfg config.Config) (map[string][]selection.SelectedCohort, error) {
	probes := selection.NewProbes(len(probeList))
	for _, p := range probeList {
		probes.Append(p)
	}
	probes.Close()

	orderer := selection.NewDefaultOrderer()
	allSelected := make(map[string][]selection.SelectedCohort, len(cfg.Measurements))
	for _, msm := range cfg.Measurements {
		selected, err := selection.Select(ctx, probes, msm.Cohorts, orderer)
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

			apiKey, err := resolveAPIKey(f.APIKey)
			if err != nil {
				return err
			}

			snapshotClient := deps.NewSnapshotClient(apiKey, f.Verbose)
			src := deps.NewProbeSource(f.SnapshotFile, snapshotClient, 0, false)
			probeList, err := src.Probes(ctx)
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

			allSelected, err := selectAll(ctx, probeList, *cfg)
			if err != nil {
				return err
			}
			codec := plan.NewTagCodec(cfg.Namespace)
			desired := plan.DesiredState(*cfg, allSelected)

			msmClient, err := deps.NewMsmClient(apiKey, f.Verbose, codec)
			if err != nil {
				return err
			}

			cs, warnings, err := plan.LiveDiff(ctx, desired, state, msmClient, codec)
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
