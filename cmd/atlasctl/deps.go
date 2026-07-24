package main

import (
	"time"

	"github.com/supabase/atlasctl/pkg/atlasapi"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// Deps bundles the factory functions used to construct API clients. Callers
// can substitute fakes for testing without touching the CLI logic.
type Deps struct {
	NewSnapshotClient func(apiKey string, verbose bool) snapshot.Client
	NewProbeSource    func(path string, client snapshot.Client, ttl time.Duration, refreshDisabled bool) snapshot.ProbeSource
	NewMsmClient      func(apiKey string, verbose bool, codec plan.TagCodec) (plan.MsmClient, error)
	NewApplyClient    func(apiKey string, verbose bool, codec plan.TagCodec) (plan.ApplyClient, error)
}

// defaultDeps returns the production Deps that connect to the live RIPE Atlas
// API via the goat library.
func defaultDeps() Deps {
	return Deps{
		NewSnapshotClient: func(_ string, verbose bool) snapshot.Client {
			return &atlasapi.ProbeClient{Verbose: verbose}
		},
		NewProbeSource: func(path string, client snapshot.Client, ttl time.Duration, refreshDisabled bool) snapshot.ProbeSource {
			return &snapshot.CachedProbeSource{
				Path:                 path,
				Client:               client,
				TTL:                  ttl,
				ProbeRefreshDisabled: refreshDisabled,
			}
		},
		NewMsmClient: func(key string, verbose bool, codec plan.TagCodec) (plan.MsmClient, error) {
			return atlasapi.NewMsmClient(key, verbose, codec)
		},
		NewApplyClient: func(key string, verbose bool, codec plan.TagCodec) (plan.ApplyClient, error) {
			return atlasapi.NewApplyClient(key, verbose, codec)
		},
	}
}
