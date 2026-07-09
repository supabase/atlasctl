package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/supabase/atlascli/pkg/config"
	"github.com/supabase/atlascli/pkg/selection"
	"github.com/supabase/atlascli/pkg/snapshot"
)

func newSelectCmd(f *globalFlags, _ Deps) *cobra.Command {
	var format string

	cmd := &cobra.Command{
		Use:   "select",
		Short: "Select probes and print coverage report",
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

			rounds, err := selection.Select(ctx, snap, *cfg)
			if err != nil {
				return err
			}

			switch format {
			case "geojson":
				data, err := selection.GeoJSON(rounds)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			default:
				report := selection.Report(rounds, cfg.Scoring)
				fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "output format: text, geojson")
	return cmd
}
