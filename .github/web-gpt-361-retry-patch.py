from pathlib import Path

path = Path("glm-worker/internal/runner/runner.go")
text = path.read_text()
old = """\tconfig      config.AppConfig
\tstate       *state.StateStore
\tstop        *StopController
\tbashSandbox *gitBashSandboxPolicy

\tinstructionSurfaceDigest string
"""
new = """\tconfig             config.AppConfig
\tstate              *state.StateStore
\tstop               *StopController
\tbashSandbox        *gitBashSandboxPolicy
\tvalidationAttempts map[string]int

\tinstructionSurfaceDigest string
"""
if text.count(old) != 1:
    raise SystemExit("runner struct anchor mismatch")
text = text.replace(old, new, 1)
old = """func NewClaudeRunner(cfg config.AppConfig, st *state.StateStore) *ClaudeRunner {
\treturn &ClaudeRunner{config: cfg, state: st}
}
"""
new = """func NewClaudeRunner(cfg config.AppConfig, st *state.StateStore) *ClaudeRunner {
\treturn &ClaudeRunner{config: cfg, state: st, validationAttempts: make(map[string]int)}
}
"""
if text.count(old) != 1:
    raise SystemExit("runner constructor anchor mismatch")
text = text.replace(old, new, 1)
old = """\tingester := newStreamEventIngester(r.state, taskID, callID, role, phase, model, sessionID, resumed)
\tingester.workerInstructionDir = filepath.Join(r.config.CodexConfigDir, \"instructions\", \"worker\")
"""
new = """\tingester := newStreamEventIngester(r.state, taskID, callID, role, phase, model, sessionID, resumed)
\tingester.validationAttempts = r.validationAttempts
\tingester.workerInstructionDir = filepath.Join(r.config.CodexConfigDir, \"instructions\", \"worker\")
"""
if text.count(old) != 1:
    raise SystemExit("runner ingester anchor mismatch")
path.write_text(text.replace(old, new, 1))

path = Path("glm-worker/internal/runner/stream_events.go")
text = path.read_text()
old = "\t\tkey := bound[index].GateClass + \"\\x00\" + bound[index].Suite + \"\\x00\" + snapshotID\n"
new = "\t\tkey := g.base.TaskID + \"\\x00\" + bound[index].GateClass + \"\\x00\" + bound[index].Suite + \"\\x00\" + snapshotID\n"
if text.count(old) != 1:
    raise SystemExit("validation attempt key anchor mismatch")
path.write_text(text.replace(old, new, 1))
