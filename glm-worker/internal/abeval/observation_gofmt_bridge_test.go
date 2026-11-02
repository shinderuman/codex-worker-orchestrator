package abeval

import (
	"encoding/base64"
	"go/format"
	"os"
	"testing"
)

func TestTemporaryObservationGofmtBridge(t *testing.T) {
	paths := []string{"../parentactioncmd/observation_execute_retention_fence_test.go",
		"../parentactioncmd/observation_execute_test.go",
		"../state/new_task_transition.go",
		"../state/observation_execution.go",
		"../state/observation_execution_fence_contract_test.go",
		"../state/observation_execution_identity_test.go",
		"../state/observation_execution_recovery.go",
		"../state/observation_execution_recovery_test.go",
		"../state/observation_execution_retention_fence_test.go",
		"../state/observation_execution_review_followup_test.go",
		"../state/observation_execution_test.go",
		"../state/task_state_lifetime.go"}
	for _, name := range paths {
		data, err := os.ReadFile(name)
		if err != nil { t.Fatalf("GFMTERROR|%s|read: %v", name, err) }
		formatted, err := format.Source(data)
		if err != nil { t.Fatalf("GFMTERROR|%s|format: %v", name, err) }
		encoded := base64.StdEncoding.EncodeToString(formatted)
		for offset, chunk := 0, 0; offset < len(encoded); offset, chunk = offset+1800, chunk+1 {
			end := offset+1800
			if end > len(encoded) { end=len(encoded) }
			t.Logf("GFMTCHUNK|%s|%d|%s", name, chunk, encoded[offset:end])
		}
	}
	t.Fatal("GFMTBRIDGE_COMPLETE")
}
