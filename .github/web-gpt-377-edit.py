from pathlib import Path
import re

root = Path('.')
wf = root/'glm-worker/internal/workflow/workflow.go'
s = wf.read_text()

def extract(start, end, path, imports):
    global s
    a = s.index(start)
    b = s.index(end, a)
    block = s[a:b].rstrip()+"\n"
    s = s[:a] + s[b:]
    Path(path).write_text('package workflow\n\nimport (\n'+imports+'\n)\n\n'+block)

extract(
    'func (w *Workflow) ExecuteResume() error {',
    'func (w *Workflow) handleWorkerResult(',
    'glm-worker/internal/workflow/resume_flow.go',
    '\t"errors"\n\t"fmt"\n\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"',
)
extract(
    'func (w *Workflow) reviewUntilStable(',
    'func (w *Workflow) runModel(',
    'glm-worker/internal/workflow/review_flow.go',
    '\t"fmt"\n\t"strings"\n\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"',
)
wf.write_text(s)

app = root/'glm-worker/internal/app/app.go'
a = app.read_text()
a = re.sub(
    r'\t\treturn Command\{\}, usageError\("usage: glm-worker <instruction> \| .*?\)\n\t\}',
    '\t\treturn Command{}, usageError("usage: glm-worker <instruction> | <command>; run glm-worker --help for command list")\n\t}',
    a,
    count=1,
)
app.write_text(a)

Path('glm-worker/internal/app/command_registry_test.go').write_text(r'''package app

import (
    "bytes"
    "encoding/json"
    "strings"
    "testing"
)

func TestNoArgUsageDelegatesCommandInventoryToHelp(t *testing.T) {
    _, err := ParseCommand(nil)
    if err == nil {
        t.Fatal("ParseCommand(nil) succeeded")
    }
    msg := err.Error()
    if !strings.Contains(msg, "glm-worker --help") {
        t.Fatalf("no-arg usage must direct callers to the registry-backed help: %q", msg)
    }
    if strings.Contains(msg, "--verify-codex-wake") {
        t.Fatalf("no-arg usage duplicated command inventory instead of delegating to help: %q", msg)
    }
}

func TestHelpCommandInventoryCoversParserRegistry(t *testing.T) {
    var out bytes.Buffer
    handled, err := runHelp([]string{"--help"}, &out)
    if err != nil || !handled {
        t.Fatalf("runHelp = handled %v err %v", handled, err)
    }
    var got helpOutput
    if err := json.Unmarshal(out.Bytes(), &got); err != nil {
        t.Fatal(err)
    }
    visible := make(map[string]bool, len(got.Commands))
    for _, command := range got.Commands {
        visible[command] = true
    }
    for command := range commandParsers {
        if command == "--decision" || command == "--fix" {
            continue
        }
        if !visible[command] {
            t.Errorf("parser registry command %q is missing from --help", command)
        }
    }
}
''')
