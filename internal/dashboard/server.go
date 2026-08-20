package dashboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/brbberry/edgelens/internal/store"
)

const defaultMeasurementLimit = 720
const maximumMeasurementLimit = 5000

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

	return mux
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
