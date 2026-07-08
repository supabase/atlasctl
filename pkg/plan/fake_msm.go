package plan

import "context"

// FakeMsmClient implements MsmClient for use in tests.
// Populate Measurements with the live API view and ListResult with
// the measurements that ListMyMeasurements should return.
type FakeMsmClient struct {
	// Measurements is the mock API state: ID → MsmInfo.
	// A missing entry causes GetMeasurement to return ErrMsmNotFound.
	Measurements map[uint64]MsmInfo

	// ListResult is returned by ListMyMeasurements.
	ListResult []MsmInfo

	// ListErr, if non-nil, is returned by ListMyMeasurements instead.
	ListErr error

	// GetErr, if non-nil, is returned by GetMeasurement instead.
	GetErr error
}

func (f *FakeMsmClient) GetMeasurement(_ context.Context, id uint64) (MsmInfo, error) {
	if f.GetErr != nil {
		return MsmInfo{}, f.GetErr
	}
	info, ok := f.Measurements[id]
	if !ok {
		return MsmInfo{}, ErrMsmNotFound
	}
	return info, nil
}

func (f *FakeMsmClient) ListMyMeasurements(_ context.Context) ([]MsmInfo, error) {
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListResult, nil
}
