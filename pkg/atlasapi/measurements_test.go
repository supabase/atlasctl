package atlasapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/google/uuid"

	"github.com/supabase/atlasctl/pkg/plan"
)

func newTestMsmClient(t *testing.T, h http.HandlerFunc) *MsmClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	k := uuid.New()
	return &MsmClient{apiKey: &k, baseURL: srv.URL + "/", httpClient: srv.Client()}
}

func TestGetMeasurement_RequestsProbes(t *testing.T) {
	var gotPath, gotOptional, gotAuth string
	c := newTestMsmClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOptional = r.URL.Query().Get("optional_fields")
		gotAuth = r.Header.Get("Authorization")
		// The real API only includes "probes" when optional_fields=probes is sent.
		if gotOptional != "probes" {
			_, _ = w.Write([]byte(`{"id":42,"type":"ping","target":"example.com","status":{"id":2,"name":"Ongoing"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":42,"type":"ping","target":"example.com","status":{"id":2,"name":"Ongoing"},
			"probes":[{"id":7,"url":"x"},{"id":3,"url":"y"}]}`))
	})

	info, err := c.GetMeasurement(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetMeasurement: %v", err)
	}
	if gotPath != "/measurements/42/" {
		t.Errorf("path = %q", gotPath)
	}
	if gotOptional != "probes" {
		t.Errorf("optional_fields = %q, want probes", gotOptional)
	}
	if gotAuth == "" {
		t.Error("missing Authorization header")
	}
	ids := append([]uint32(nil), info.ProbeIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 7 {
		t.Errorf("ProbeIDs = %v, want [3 7]", info.ProbeIDs)
	}
	if info.ID != 42 || info.Type != "ping" || info.Target != "example.com" {
		t.Errorf("unexpected info: %+v", info)
	}
}

func TestGetMeasurement_NotFound(t *testing.T) {
	c := newTestMsmClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":404,"code":104,"title":"Not found"}}`))
	})
	_, err := c.GetMeasurement(context.Background(), 1)
	if !errors.Is(err, plan.ErrMsmNotFound) {
		t.Fatalf("err = %v, want ErrMsmNotFound", err)
	}
}

func TestGetMeasurement_ServerError(t *testing.T) {
	c := newTestMsmClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, err := c.GetMeasurement(context.Background(), 1)
	if err == nil || errors.Is(err, plan.ErrMsmNotFound) {
		t.Fatalf("err = %v, want non-NotFound error", err)
	}
}

func TestGetMeasurement_CanceledContext(t *testing.T) {
	c := newTestMsmClient(t, func(http.ResponseWriter, *http.Request) {})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetMeasurement(ctx, 1); err == nil {
		t.Fatal("expected error for canceled context")
	}
}
