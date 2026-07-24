package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

func newSelectCmd(f *globalFlags, deps Deps) *cobra.Command {
	var format string
	var geojsonLink bool

	cmd := &cobra.Command{
		Use:   "select",
		Short: "Select probes and print coverage report",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			cfg, err := config.Load(f.ConfigPath)
			if err != nil {
				return err
			}

			// API key is optional for select: if present, enables transparent
			// cache refresh; if absent, the existing on-disk snapshot is used.
			apiKey := resolveAPIKeyOptional(f.APIKey)
			var client snapshot.Client
			if apiKey != "" {
				client = deps.NewSnapshotClient(apiKey, f.Verbose)
			}
			src := deps.NewProbeSource(f.SnapshotFile, client, 0, client == nil)
			probeList, err := src.Probes(ctx)
			if err != nil {
				return err
			}

			allSelected, err := selectAll(ctx, probeList, *cfg)
			if err != nil {
				return err
			}

			// Flatten per-measurement results into a single ordered slice for
			// reporting and GeoJSON, preserving measurement definition order.
			var cohorts []selection.SelectedCohort
			for _, msm := range cfg.Measurements {
				cohorts = append(cohorts, allSelected[msm.Name]...)
			}

			data, err := selection.GeoJSON(cohorts)
			if err != nil {
				return err
			}

			if geojsonLink {
				for _, r := range cohorts {
					cohortData, err := selection.GeoJSONCohort(r)
					if err != nil {
						return err
					}
					link := "https://geojson.io/#data=data:application/json," + url.QueryEscape(string(cohortData))
					fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", r.Cohort.Name, link)
				}
				return nil
			}

			switch format {
			case "geojson":
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			default:
				report := selection.Report(cohorts)
				fmt.Fprintln(cmd.OutOrStdout(), report.Format())
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "text", "output format: text, geojson")
	cmd.Flags().BoolVar(&geojsonLink, "geojson-link", false,
		"print a geojson.io URL with probe locations encoded")
	return cmd
}
