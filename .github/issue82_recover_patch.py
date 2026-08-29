from pathlib import Path

p = Path("glm-worker/internal/app/output.go")
text = p.read_text()
old = '''func parentAccept(st *state.StateStore, stdout io.Writer) error {
\tresolved, err := st.RecordParentOutcome(state.ParentOutcomeAccepted, "")
\tif err != nil {
\t\treturn err
\t}
\tif resolved {
\t\tif err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
\t\t\treturn err
\t\t}
\t}
\treturn writeJSON(stdout, acceptOutput{Accepted: resolved})
}
'''
new = '''func parentAccept(st *state.StateStore, stdout io.Writer) error {
\tresolved, err := st.AcceptParentReview()
\tif err != nil {
\t\treturn err
\t}
\treturn writeJSON(stdout, acceptOutput{Accepted: resolved})
}
'''
if old not in text:
    raise SystemExit("parentAccept patch point not found")
p.write_text(text.replace(old, new, 1))
