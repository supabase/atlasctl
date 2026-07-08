package snapshot

import "context"

// FakeClient implements Client for use in tests.
// Callers set Probes to the list to return, or Err to force an error.
type FakeClient struct {
	Probes []Probe
	Err    error
}

func (f *FakeClient) FetchProbes(_ context.Context) ([]Probe, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Probes, nil
}
