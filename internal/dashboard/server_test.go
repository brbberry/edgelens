package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/brbberry/edgelens/internal/store"
	"github.com/brbberry/edgelens/internal/wire"
)

func TestMeasurementsAPI(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "measurements.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	for _, timestamp := range []int64{100, 200} {
		if err := database.WriteMeasurement(context.Background(), wire.Measurement{
			Version: wire.Version, Host: "edge-01", Timestamp: timestamp,
		}); err != nil {
			t.Fatal(err)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/api/measurements?limit=1", nil)
	response := httptest.NewRecorder()
	NewHandler(database).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got, want := response.Header().Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := response.Body.String(), "[{\"v\":1,\"host\":\"edge-01\",\"ts\":200"; len(got) < len(want) || got[:len(want)] != want {
		t.Fatalf("response body = %q, want it to start with %q", got, want)
	}
}
