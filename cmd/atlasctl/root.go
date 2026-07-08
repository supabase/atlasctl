package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// globalFlags holds flags shared across all subcommands via PersistentFlags.
type globalFlags struct {
	ConfigPath   string
	StateFile    string
	SnapshotFile string
	APIKey       string
	LogLevel     string
	Verbose      bool
}

func newRootCmd(deps Deps) *cobra.Command {
	var f globalFlags

	root := &cobra.Command{
		Use:           "atlasctl",
		Short:         "Declarative management of RIPE Atlas measurements",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initLogger(cmd, f.LogLevel)
		},
	}

	root.PersistentFlags().StringVar(&f.ConfigPath, "config", "atlasctl.yaml", "path to config file")
	root.PersistentFlags().StringVar(&f.StateFile, "state", "state.yaml", "path to state file")
	root.PersistentFlags().StringVar(&f.SnapshotFile, "snapshot", "snapshot.json", "path to probe snapshot file")
	root.PersistentFlags().StringVar(&f.APIKey, "api-key", "", "RIPE Atlas API key (overrides RIPE_ATLAS_API_KEY env var)")
	root.PersistentFlags().StringVar(&f.LogLevel, "log-level", "info", "log level: debug, info, warn, error")
	root.PersistentFlags().BoolVar(&f.Verbose, "verbose", false, "enable verbose goat API output")

	root.AddCommand(
		newRefreshCmd(&f, deps),
		newSelectCmd(&f, deps),
		newPlanCmd(&f, deps),
		newApplyCmd(&f, deps),
	)

	return root
}

// initLogger constructs a zerolog.Logger, stores it in cmd's context, and
// writes structured JSON to cmd's stderr writer.
func initLogger(cmd *cobra.Command, level string) error {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	logger := zerolog.New(cmd.ErrOrStderr()).Level(lvl).With().Timestamp().Logger()
	cmd.SetContext(logger.WithContext(cmd.Context()))
	return nil
}

// resolveAPIKey returns the API key from the flag value or the
// RIPE_ATLAS_API_KEY environment variable. Returns an error if neither is set
// or if the value is not a valid UUID.
func resolveAPIKey(flagVal string) (*uuid.UUID, error) {
	s := flagVal
	if s == "" {
		s = os.Getenv("RIPE_ATLAS_API_KEY")
	}
	if s == "" {
		return nil, errors.New("RIPE Atlas API key required: use --api-key or set RIPE_ATLAS_API_KEY")
	}
	k, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid API key (expected UUID): %w", err)
	}
	return &k, nil
}
