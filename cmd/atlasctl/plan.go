package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

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

			cohorts, err := selection.Select(ctx, snap, *cfg)
			if err != nil {
				return err
			}
			desired := plan.DesiredState(*cfg, cohorts)

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
