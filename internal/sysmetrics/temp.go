package sysmetrics

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// TempZone holds one thermal zone's type label and temperature in Celsius.
type TempZone struct {
	Zone    string
	Type    string
	Celsius float64
}

// ReadTemps reads every /sys/class/thermal/thermal_zone*/temp file and
// returns the parsed temperatures. Zones that error while reading are
// skipped, since not every zone is populated on every board.
func ReadTemps() (TempZone, error) {
	paths, err := filepath.Glob("/sys/class/thermal/thermal_zone*")
	if err != nil {
		return TempZone{}, fmt.Errorf("globbing thermal zones: %w", err)
	}

	for _, zonePath := range paths {
		rawMilliC, err := os.ReadFile(filepath.Join(zonePath, "temp"))
		if err != nil {
			continue
		}
		milliC, err := strconv.ParseInt(strings.TrimSpace(string(rawMilliC)), 10, 64)
		if err != nil {
			continue
		}

		typeLabel, err := os.ReadFile(filepath.Join(zonePath, "type"))
		if err != nil {
			typeLabel = []byte("unknown")
		}

		return TempZone{
			Zone: filepath.Base(zonePath),
			Type: strings.TrimSpace(string(typeLabel)),
			// the kernel reports temp in millidegrees Celsius
			Celsius: float64(milliC) / 1000,
		}, nil
	}
	return TempZone{}, fmt.Errorf("no thermal zones found")
}
