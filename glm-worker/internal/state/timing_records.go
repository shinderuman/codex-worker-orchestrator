package state

import (
	"encoding/json"
	"fmt"
	"os"
)

func readTimingRecords[T any](path string, currentVersion int, version func(T) int) ([]T, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []T
	for _, line := range splitJSONLines(data) {
		var record T
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("timing recordを読めません: %w", err)
		}
		if version(record) != currentVersion {
			return nil, fmt.Errorf("timing record: unsupported version %d", version(record))
		}
		records = append(records, record)
	}
	return records, nil
}
