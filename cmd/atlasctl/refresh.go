package main

import (
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/supabase/atlasctl/pkg/snapshot"
)

func newRefreshCmd(f *globalFlags, deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh",
		Short: "Fetch the probe snapshot from the RIPE Atlas API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			log := zerolog.Ctx(ctx)

			apiKey, err := resolveAPIKey(f.APIKey)
			if err != nil {
				return err
			}

			client := deps.NewSnapshotClient(apiKey, f.Verbose)
			probes, err := client.FetchProbes(ctx)
			if err != nil {
				return err
			}

			snap := snapshot.Snapshot{Probes: probes, FetchedAt: time.Now().UTC()}
			if err := snapshot.Save(f.SnapshotFile, snap); err != nil {
				return err
			}

			log.Info().Int("probes", len(probes)).Str("path", f.SnapshotFile).Msg("snapshot saved")
			return nil
		},
	}
}
