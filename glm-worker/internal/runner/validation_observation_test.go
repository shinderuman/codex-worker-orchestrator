package runner

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestValidationObservationsPreserveKnownValidationAcrossUnsupportedShellShapes(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{name: "env prefix and pipe", command: "GOFLAGS= go test ./... 2>&1 | tail -20", want: []string{"go-test"}},
		{name: "env command race and pipe", command: "env -u ANTHROPIC_API_KEY FOO=1 go test -race ./... | cat", want: []string{"go-test-race"}},
		{name: "compound validations", command: "go build ./...; go test ./internal/foo && ./harnesslint", want: []string{"go-build", "go-test", "harnesslint"}},
		{name: "lint forms", command: "./commentlint && ./harnesslint", want: []string{"commentlint", "harnesslint"}},
		{name: "quoted mention is not command", command: "printf 'go test ./...'", want: nil},
		{name: "quoted separator does not create command", command: "printf 'x|go test ./...'", want: nil},
		{name: "quality gate wrapper is not duplicated", command: "glm-worker --quality-gate go-test", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := validationObservationForms(validationObservationsForCommand(test.command))
			if !slices.Equal(got, test.want) {
				t.Fatalf("forms = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidationObservationsDoNotWidenOperationCategory(t *testing.T) {
	command := "GOFLAGS= go test ./... 2>&1 | tail -20"
	if got := shellOperationCategory(command); got != state.OperationCategoryOther {
		t.Fatalf("operation category = %q, want other", got)
	}
	if got := validationObservationForms(validationObservationsForCommand(command)); !slices.Equal(got, []string{"go-test"}) {
		t.Fatalf("validation forms = %v", got)
	}
}

func TestReduceStreamBlockStoresValidationWithoutRawCommand(t *testing.T) {
	input, err := json.Marshal(map[string]string{"command": "TOKEN=secret go test -race ./... | tail -1"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"type":  "tool_use",
		"name":  "Bash",
		"id":    "tool-1",
		"input": json.RawMessage(input),
	})
	if err != nil {
		t.Fatal(err)
	}
	blocks := reduceStreamBlocks([]json.RawMessage{raw})
	if len(blocks) != 1 {
		t.Fatalf("blocks = %d", len(blocks))
	}
	if got := validationObservationForms(blocks[0].Validation); !slices.Equal(got, []string{"go-test-race"}) {
		t.Fatalf("validation = %v", got)
	}
	encoded, err := json.Marshal(blocks[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "TOKEN=secret") {
		t.Fatalf("raw command leaked: %s", encoded)
	}
}

func validationObservationForms(values []state.TaskValidationObservation) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Form)
	}
	return result
}
