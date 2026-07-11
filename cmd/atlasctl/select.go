package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/config"
	"github.com/supabase/atlasctl/pkg/selection"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

func newSelectCmd(f *globalFlags, _ Deps) *cobra.Command {
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

			snap, err := snapshot.Load(f.SnapshotFile)
			if err != nil {
				return err
			}

			cohorts, err := selection.Select(ctx, snap, *cfg)
			if err != nil {
				return err
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
				report := selection.Report(cohorts, cfg.Scoring)
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
