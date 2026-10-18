package labs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IEC104Point is a station point's identity in protocols/points.json.
// Protocol/source configuration remains owned by the IEC 104 services.
type IEC104Point struct {
	ID  string `json:"id"`
	IOA int    `json:"ioa"`
}

func loadIEC104Points(packageDir string) ([]IEC104Point, error) {
	path := filepath.Join(packageDir, "protocols", "points.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("station inventory: %w", err)
	}
	var inventory struct {
		Schema int           `json:"schema"`
		Points []IEC104Point `json:"points"`
	}
	if err := json.Unmarshal(data, &inventory); err != nil {
		return nil, fmt.Errorf("station inventory %s: %w", path, err)
	}
	if inventory.Schema != 2 || len(inventory.Points) == 0 {
		return nil, fmt.Errorf("station inventory %s: schema must be 2 and points must not be empty", path)
	}
	ids, ioas := map[string]bool{}, map[int]bool{}
	for _, point := range inventory.Points {
		if strings.TrimSpace(point.ID) == "" || point.IOA < 1 || point.IOA > 0xffffff {
			return nil, fmt.Errorf("station inventory %s: invalid identity %+v", path, point)
		}
		if ids[point.ID] || ioas[point.IOA] {
			return nil, fmt.Errorf("station inventory %s: duplicate identity %+v", path, point)
		}
		ids[point.ID], ioas[point.IOA] = true, true
	}
	return inventory.Points, nil
}
