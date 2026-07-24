package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/plan"
)

func newApplyCmd(f *globalFlags, deps Deps) *cobra.Command {
	var dryRun bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply changes to RIPE Atlas measurements",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			log := *zerolog.Ctx(ctx)

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
			desired := plan.DesiredState(*cfg, allSelected)

			applyClient, err := deps.NewApplyClient(apiKey, f.Verbose, plan.NewTagCodec(cfg.Namespace))
			if err != nil {
				return err
			}

			cs, warnings, err := plan.LiveDiff(ctx, desired, state, applyClient)
			if err != nil {
				return err
			}

			printChangeset(cmd.OutOrStdout(), cs)
			printWarnings(cmd.OutOrStdout(), warnings)

			if !dryRun && !yes {
				fmt.Fprint(cmd.OutOrStdout(), "\nApply changes? [y/N] ")
				var response string
				_, _ = fmt.Fscan(cmd.InOrStdin(), &response)
				if strings.ToLower(strings.TrimSpace(response)) != "y" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			opts := plan.ApplyOptions{DryRun: dryRun}
			newState, applyErr := plan.Apply(ctx, cs, state, applyClient, opts, log)

			if !dryRun {
				if saveErr := plan.SaveState(f.StateFile, newState); saveErr != nil {
					return fmt.Errorf("saving state: %w", saveErr)
				}
			}

			return applyErr
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print what would happen without making API calls")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply without prompting for confirmation")
	return cmd
}
