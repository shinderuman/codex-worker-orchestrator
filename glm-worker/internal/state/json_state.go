package state

import (
	"encoding/json"
	"fmt"
	"os"
)

func writeJSONStateFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("stateをJSON化できません: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func readJSONStateFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("state JSONを読めません: %w", err)
	}
	return nil
}
