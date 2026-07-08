package plan

import "context"

// FakeApplyClient implements ApplyClient for use in tests.
// Embed FakeMsmClient for the read-only MsmClient methods.
type FakeApplyClient struct {
	FakeMsmClient

	// CreateMeasurement tracking.
	CreatedSpecs []MsmSpec
	// CreateIDs provides IDs to return in order; defaults to 10_000_001, 10_000_002, …
	CreateIDs []uint64
	CreateErr error
	createN   int

	// StopMeasurement tracking.
	StoppedIDs []uint64
	StopErr    error

	// AddParticipants tracking.
	AddedCalls []ParticipationCall
	AddErr     error

	// RemoveParticipants tracking.
	RemovedCalls []ParticipationCall
	RemoveErr    error
}

// ParticipationCall records one AddParticipants or RemoveParticipants call.
type ParticipationCall struct {
	MsmID    uint64
	ProbeIDs []uint32
}

func (f *FakeApplyClient) CreateMeasurement(_ context.Context, spec MsmSpec) (uint64, error) {
	if f.CreateErr != nil {
		return 0, f.CreateErr
	}
	f.CreatedSpecs = append(f.CreatedSpecs, spec)
	var id uint64
	if f.createN < len(f.CreateIDs) {
		id = f.CreateIDs[f.createN]
	} else {
		id = uint64(10_000_001 + f.createN)
	}
	f.createN++
	return id, nil
}

func (f *FakeApplyClient) StopMeasurement(_ context.Context, id uint64) error {
	if f.StopErr != nil {
		return f.StopErr
	}
	f.StoppedIDs = append(f.StoppedIDs, id)
	return nil
}

func (f *FakeApplyClient) AddParticipants(_ context.Context, id uint64, probeIDs []uint32) error {
	if f.AddErr != nil {
		return f.AddErr
	}
	f.AddedCalls = append(f.AddedCalls, ParticipationCall{MsmID: id, ProbeIDs: probeIDs})
	return nil
}

func (f *FakeApplyClient) RemoveParticipants(_ context.Context, id uint64, probeIDs []uint32) error {
	if f.RemoveErr != nil {
		return f.RemoveErr
	}
	f.RemovedCalls = append(f.RemovedCalls, ParticipationCall{MsmID: id, ProbeIDs: probeIDs})
	return nil
}
