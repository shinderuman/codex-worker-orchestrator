package runner

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestValidationCommandResultAttribution(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{name: "simple", command: "go test ./...", want: true},
		{name: "env and redirect", command: "FOO=1 go test ./... 2>/tmp/test.log", want: true},
		{name: "pipeline", command: "go test ./... 2>&1 | tail -20", want: false},
		{name: "pipeline chain", command: "go test ./... | grep ok | head -1", want: false},
		{name: "semicolon", command: "go test ./...; echo done", want: false},
		{name: "and chain", command: "go test ./... && echo done", want: false},
		{name: "or mask", command: "go test ./... || true", want: false},
		{name: "background", command: "go test ./... &", want: false},
		{name: "stderr redirect ampersand", command: "go test ./... 2>&1", want: true},
		{name: "bash redirect ampersand", command: "go test ./... &>/tmp/test.log", want: true},
		{name: "quoted pipeline", command: "go test './pkg|name'", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validationCommandResultAttributable(test.command); got != test.want {
				t.Fatalf("attributable = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidationToolResultUsesOnlyTruthfulExitAttribution(t *testing.T) {
	forms := []state.TaskValidationObservation{{Form: "go-test"}}
	if got := validationObservationsWithToolResult(forms, true, false); len(got) != 1 || got[0].Result != state.ValidationResultPass {
		t.Fatalf("simple pass = %#v", got)
	}
	if got := validationObservationsWithToolResult(forms, true, true); len(got) != 1 || got[0].Result != state.ValidationResultFail {
		t.Fatalf("simple fail = %#v", got)
	}
	if got := validationObservationsWithToolResult(forms, false, false); len(got) != 1 || got[0].Result != state.ValidationResultUnknown {
		t.Fatalf("masked result = %#v", got)
	}
}

func TestFailingGoTestPipedToTailDoesNotBecomeValidationPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell pipeline fixture is Unix-oriented")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/pipeline\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fail_test.go"), []byte("package pipeline\n\nimport \"testing\"\n\nfunc TestFails(t *testing.T) { t.Fatal(\"boom\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commandText := "go test ./... 2>&1 | tail -1"
	command := exec.Command("sh", "-c", commandText)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("pipeline should mask failing go test at shell boundary: %v: %s", err, output)
	}
	input, err := json.Marshal(map[string]string{"command": commandText})
	if err != nil {
		t.Fatal(err)
	}
	forms := validationObservationsForToolInput("Bash", input)
	if len(forms) != 1 || forms[0].Form != "go-test" {
		t.Fatalf("forms = %#v", forms)
	}
	if validationToolResultAttributable("Bash", input) {
		t.Fatal("masked pipeline was treated as attributable")
	}
	result := validationObservationsWithToolResult(forms, false, false)
	if len(result) != 1 || result[0].Result != state.ValidationResultUnknown {
		t.Fatalf("masked pipeline result = %#v", result)
	}
}
