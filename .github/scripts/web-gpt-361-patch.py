from pathlib import Path

path = Path("glm-worker/internal/runner/stream_events.go")
text = path.read_text()
old = "\ttools                map[string]toolUseObservation\n\tinstructionReads     map[string]struct{}\n"
new = "\ttools                map[string]toolUseObservation\n\tvalidationAttempts   map[string]int\n\tinstructionReads     map[string]struct{}\n"
if text.count(old) != 1:
    raise SystemExit("stream ingester field anchor mismatch")
text = text.replace(old, new, 1)

old = "\t\ttools:            make(map[string]toolUseObservation),\n\t\tinstructionReads: make(map[string]struct{}),\n"
new = "\t\ttools:              make(map[string]toolUseObservation),\n\t\tvalidationAttempts: make(map[string]int),\n\t\tinstructionReads:   make(map[string]struct{}),\n"
if text.count(old) != 1:
    raise SystemExit("stream ingester init anchor mismatch")
text = text.replace(old, new, 1)

old = "\tobservation.category = operationCategoryForTool(block.Name, observation.command)\n\tobservation.validation = append([]state.TaskValidationObservation(nil), block.Validation...)\n\tblock.OperationCategory = observation.category\n"
new = "\tobservation.category = operationCategoryForTool(block.Name, observation.command)\n\tobservation.validation = g.bindValidationObservations(block.Validation)\n\tblock.OperationCategory = observation.category\n\tblock.Validation = append([]state.TaskValidationObservation(nil), observation.validation...)\n"
if text.count(old) != 1:
    raise SystemExit("stream validation binding anchor mismatch")
text = text.replace(old, new, 1)

anchor = "func workerInstructionReadName(toolName string, input json.RawMessage, instructionDir string) (string, bool) {\n"
helper = '''func (g *streamEventIngester) bindValidationObservations(values []state.TaskValidationObservation) []state.TaskValidationObservation {
\tif len(values) == 0 {
\t\treturn nil
\t}
\tbound := append([]state.TaskValidationObservation(nil), values...)
\tsnapshotID := ""
\tif repoRoot := g.state.ReadOr("repo-root", ""); repoRoot != "" {
\t\tif snapshot, err := state.CaptureGitSnapshot(repoRoot); err == nil {
\t\t\tsnapshotID = state.ValidationSnapshotID(snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest)
\t\t}
\t}
\tfor index := range bound {
\t\tif bound[index].Suite == "" {
\t\t\tbound[index].Suite = bound[index].Form
\t\t}
\t\tif bound[index].GateClass == "" {
\t\t\tbound[index].GateClass = state.ValidationGateClass(bound[index].Suite)
\t\t}
\t\tbound[index].SnapshotID = snapshotID
\t\tbound[index].Phase = g.base.Phase
\t\tkey := bound[index].GateClass + "\\x00" + bound[index].Suite + "\\x00" + snapshotID
\t\tif g.validationAttempts[key] == 0 {
\t\t\tbound[index].Attempt = state.ValidationAttemptInitial
\t\t} else {
\t\t\tbound[index].Attempt = state.ValidationAttemptRetry
\t\t}
\t\tg.validationAttempts[key]++
\t}
\treturn bound
}

'''
if text.count(anchor) != 1:
    raise SystemExit("stream helper anchor mismatch")
text = text.replace(anchor, helper + anchor, 1)
path.write_text(text)

path = Path("glm-worker/internal/app/quality_gate.go")
text = path.read_text()
old = '''func recordQualityGateValidation(st *state.StateStore, record qualityGateRunRecord) {
\tevidence := ""
\tif record.Log != "" {
\t\tevidence = filepath.ToSlash(filepath.Join(qualityGateRunDirectory, record.ValidationRunID, qualityGateRunLog))
\t}
\tst.RecordValidation("quality-gate", record.Form, "", record.Status, record.ExitCode, record.ExitSource, record.DurationMS, evidence)
}
'''
new = '''func recordQualityGateValidation(st *state.StateStore, record qualityGateRunRecord) {
\tevidence := ""
\tif record.Log != "" {
\t\tevidence = filepath.ToSlash(filepath.Join(qualityGateRunDirectory, record.ValidationRunID, qualityGateRunLog))
\t}
\tst.RecordValidationEvent(state.TaskValidationEvent{
\t\tSource:          "quality-gate",
\t\tForm:            record.Form,
\t\tValidationRunID: record.ValidationRunID,
\t\tGateClass:       state.ValidationGateClass(record.Form),
\t\tSuite:           record.Form,
\t\tSnapshotID:      state.ValidationSnapshotID(record.Head, record.IndexDigest, record.WorktreeDigest),
\t\tPhase:           "quality-gate",
\t\tAttempt:         state.ValidationAttemptInitial,
\t\tResult:          record.Status,
\t\tExitCode:        record.ExitCode,
\t\tExitSource:      record.ExitSource,
\t\tDurationMS:      record.DurationMS,
\t\tEvidence:        evidence,
\t})
}
'''
if text.count(old) != 1:
    raise SystemExit("quality gate validation anchor mismatch")
path.write_text(text.replace(old, new, 1))
