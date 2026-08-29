from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"patch point not found: {path}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/state/events.go",
    '''type TaskValidationObservation struct {
\tForm string `json:"form"`
}
''',
    '''type TaskValidationObservation struct {
\tForm   string `json:"form"`
\tResult string `json:"result,omitempty"`
}

const (
\tValidationResultPass    = "pass"
\tValidationResultFail    = "fail"
\tValidationResultUnknown = "unknown"
)
''',
)

replace_once(
    "glm-worker/internal/runner/stream_events.go",
    '''type toolUseObservation struct {
\ttoolID          string
\ttimestamp       time.Time
\tname            string
\tcommand         string
\tcategory        string
\tpurpose         string
\tbackground      bool
\twaitTaskID      string
\tinstructionRead string
}
''',
    '''type toolUseObservation struct {
\ttoolID                       string
\ttimestamp                    time.Time
\tname                         string
\tcommand                      string
\tcategory                     string
\tpurpose                      string
\tbackground                   bool
\twaitTaskID                   string
\tinstructionRead              string
\tvalidation                   []state.TaskValidationObservation
\tvalidationResultAttributable bool
}
''',
)

replace_once(
    "glm-worker/internal/runner/stream_events.go",
    '''\tif input, ok := inputs[block.ToolID]; ok {
\t\tdetail := extractLiveToolDetail(input)
\t\tobservation.command = detail.command
\t\tobservation.purpose = detail.purpose
\t\tobservation.background = detail.background
\t\tobservation.waitTaskID = detail.waitTaskID
\t\tif name, matched := workerInstructionReadName(observation.name, input, g.workerInstructionDir); matched {
\t\t\tobservation.instructionRead = name
\t\t}
\t}
\tobservation.category = operationCategoryForTool(block.Name, observation.command)
\tblock.OperationCategory = observation.category
\tg.tools[block.ToolID] = observation
''',
    '''\tif input, ok := inputs[block.ToolID]; ok {
\t\tdetail := extractLiveToolDetail(input)
\t\tobservation.command = detail.command
\t\tobservation.purpose = detail.purpose
\t\tobservation.background = detail.background
\t\tobservation.waitTaskID = detail.waitTaskID
\t\tobservation.validationResultAttributable = validationToolResultAttributable(block.Name, input)
\t\tif name, matched := workerInstructionReadName(observation.name, input, g.workerInstructionDir); matched {
\t\t\tobservation.instructionRead = name
\t\t}
\t}
\tobservation.category = operationCategoryForTool(block.Name, observation.command)
\tobservation.validation = append([]state.TaskValidationObservation(nil), block.Validation...)
\tblock.OperationCategory = observation.category
\tg.tools[block.ToolID] = observation
''',
)

replace_once(
    "glm-worker/internal/runner/stream_events.go",
    '''\tblock.OperationCategory = observed.category
\tif !block.IsError && observed.instructionRead != "" {
''',
    '''\tblock.OperationCategory = observed.category
\tblock.Validation = validationObservationsWithToolResult(observed.validation, observed.validationResultAttributable, block.IsError)
\tif !block.IsError && observed.instructionRead != "" {
''',
)

p = Path("glm-worker/internal/runner/validation_observation.go")
text = p.read_text()
append = r'''

func validationToolResultAttributable(toolName string, input json.RawMessage) bool {
	if toolName != "Bash" || len(input) == 0 {
		return false
	}
	var parsed struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Command == "" {
		return false
	}
	return validationCommandResultAttributable(parsed.Command)
}

func validationCommandResultAttributable(command string) bool {
	if len(validationObservationsForCommand(command)) == 0 {
		return false
	}
	scanner := validationSegmentScanner{command: command}
	for index := 0; index < len(command); index++ {
		if width := scanner.separatorWidth(index); width > 0 {
			return false
		}
		if command[index] == '&' && !scanner.singleQuoted && !scanner.doubleQuoted && validationBackgroundAmpersand(command, index) {
			return false
		}
	}
	return true
}

func validationBackgroundAmpersand(command string, index int) bool {
	if index > 0 && (command[index-1] == '>' || command[index-1] == '<') {
		return false
	}
	if index+1 < len(command) && command[index+1] == '>' {
		return false
	}
	return true
}

func validationObservationsWithToolResult(values []state.TaskValidationObservation, attributable bool, isError bool) []state.TaskValidationObservation {
	if len(values) == 0 {
		return nil
	}
	resultValue := state.ValidationResultUnknown
	if attributable {
		if isError {
			resultValue = state.ValidationResultFail
		} else {
			resultValue = state.ValidationResultPass
		}
	}
	result := make([]state.TaskValidationObservation, len(values))
	copy(result, values)
	for index := range result {
		result[index].Result = resultValue
	}
	return result
}
'''
if "func validationToolResultAttributable(" in text:
    raise SystemExit("issue79 helpers already present")
p.write_text(text + append)

Path("glm-worker/internal/runner/validation_result_test.go").write_text(r'''package runner

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
''')
