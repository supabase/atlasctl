package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/plan"
	"github.com/supabase/atlascli/pkg/selection"
	"github.com/supabase/atlascli/pkg/snapshot"
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

			rounds, err := selection.Select(ctx, snap, *cfg)
			if err != nil {
				return err
			}
			desired := plan.DesiredState(*cfg, rounds)

			apiKey, err := resolveAPIKey(f.APIKey)
			if err != nil {
				return err
			}
			msmClient := deps.NewMsmClient(apiKey, f.Verbose)

			cs, warnings, err := plan.LiveDiff(ctx, desired, state, msmClient)
			if err != nil {
				return err
			}

			printChangeset(cmd.OutOrStdout(), cs)
			printWarnings(cmd.OutOrStdout(), warnings)
			return nil
		},
	}
}
