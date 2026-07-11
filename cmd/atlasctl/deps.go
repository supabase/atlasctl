package main

import (
	"github.com/google/uuid"

	"github.com/supabase/atlasctl/internal/goatadapter"
	"github.com/supabase/atlasctl/pkg/plan"
	"github.com/supabase/atlasctl/pkg/snapshot"
)

// Deps bundles the factory functions used to construct API clients. Callers
// can substitute fakes for testing without touching the CLI logic.
type Deps struct {
	NewSnapshotClient func(apiKey *uuid.UUID, verbose bool) snapshot.Client
	NewMsmClient      func(apiKey *uuid.UUID, verbose bool, codec plan.TagCodec) plan.MsmClient
	NewApplyClient    func(apiKey *uuid.UUID, verbose bool, codec plan.TagCodec) plan.ApplyClient
}

// defaultDeps returns the production Deps that connect to the live RIPE Atlas
// API via the goat library.
func defaultDeps() Deps {
	return Deps{
		NewSnapshotClient: func(_ *uuid.UUID, verbose bool) snapshot.Client {
			return &goatadapter.ProbeClient{Verbose: verbose}
		},
		NewMsmClient: func(key *uuid.UUID, verbose bool, codec plan.TagCodec) plan.MsmClient {
			return &goatadapter.MsmClient{APIKey: key, Verbose: verbose, TagCodec: codec}
		},
		NewApplyClient: func(key *uuid.UUID, verbose bool, codec plan.TagCodec) plan.ApplyClient {
			return goatadapter.NewApplyClient(key, verbose, codec)
		},
	}
}
