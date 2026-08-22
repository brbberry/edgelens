package dashboard

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/brbberry/edgelens/internal/store"
)

const defaultMeasurementLimit = 720
const maximumMeasurementLimit = 5000
const defaultRunLimit = 50
const maximumRunLimit = 500
const defaultProcessSampleLimit = 2000
const maximumProcessSampleLimit = 10000

//go:embed web/index.html
var indexHTML []byte

// NewHandler returns the HTTP dashboard and its measurement API.
func NewHandler(database *store.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = writer.Write(indexHTML)
	})
	mux.HandleFunc("/api/measurements", func(writer http.ResponseWriter, request *http.Request) {
		limit, err := measurementLimit(request)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}

		measurements, err := database.ReadMeasurements(request.Context(), limit)
		if err != nil {
			http.Error(writer, "read measurements", http.StatusInternalServerError)
			return
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(writer).Encode(measurements); err != nil {
			return
		}
	})
	mux.HandleFunc("GET /api/runs", func(writer http.ResponseWriter, request *http.Request) {
		limit, err := boundedLimit(request, defaultRunLimit, maximumRunLimit)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		runs, err := database.ListRuns(request.Context(), request.URL.Query().Get("host"), limit)
		if err != nil {
			http.Error(writer, "read runs", http.StatusInternalServerError)
			return
		}
		writeJSON(writer, runs)
	})
	mux.HandleFunc("GET /api/runs/{id}", func(writer http.ResponseWriter, request *http.Request) {
		runID, ok := validPathValue(request.PathValue("id"))
		if !ok {
			http.Error(writer, "invalid run ID", http.StatusBadRequest)
			return
		}
		run, err := database.ReadRun(request.Context(), runID)
		if writeStoreError(writer, err, "read run") {
			return
		}
		writeJSON(writer, run)
	})
	mux.HandleFunc("GET /api/runs/{id}/process-samples", func(writer http.ResponseWriter, request *http.Request) {
		runID, ok := validPathValue(request.PathValue("id"))
		if !ok {
			http.Error(writer, "invalid run ID", http.StatusBadRequest)
			return
		}
		limit, err := boundedLimit(request, defaultProcessSampleLimit, maximumProcessSampleLimit)
		if err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		samples, err := database.ReadProcessSamples(request.Context(), runID, limit)
		if writeStoreError(writer, err, "read process samples") {
			return
		}
		writeJSON(writer, samples)
	})
	mux.HandleFunc("GET /api/runs/{id}/artifacts/{kind}", func(writer http.ResponseWriter, request *http.Request) {
		runID, runOK := validPathValue(request.PathValue("id"))
		kind, kindOK := validPathValue(request.PathValue("kind"))
		if !runOK || !kindOK || (kind != "perf-stat" && kind != "flame-folded" && kind != "heap-summary") {
			http.Error(writer, "invalid run or artifact kind", http.StatusBadRequest)
			return
		}
		artifact, err := database.ReadArtifact(request.Context(), runID, kind)
		if writeStoreError(writer, err, "read artifact") {
			return
		}
		writeJSON(writer, artifact)
	})

	return mux
}

func boundedLimit(request *http.Request, defaultLimit, maximum int) (int, error) {
	text := request.URL.Query().Get("limit")
	if text == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(text)
	if err != nil || limit <= 0 || limit > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return limit, nil
}

func validPathValue(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	return trimmed, trimmed != "" && len(trimmed) <= 200 && !strings.ContainsAny(trimmed, "/\\\x00")
}

func writeStoreError(writer http.ResponseWriter, err error, operation string) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		http.Error(writer, "not found", http.StatusNotFound)
		return true
	}
	http.Error(writer, operation, http.StatusInternalServerError)
	return true
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(writer).Encode(value)
}

func measurementLimit(request *http.Request) (int, error) {
	limitText := request.URL.Query().Get("limit")
	if limitText == "" {
		return defaultMeasurementLimit, nil
	}

	limit, err := strconv.Atoi(limitText)
	if err != nil || limit <= 0 || limit > maximumMeasurementLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximumMeasurementLimit)
	}
	return limit, nil
}
