from pathlib import Path
p = Path('glm-worker/internal/app/multirepo_process_test.go')
s = p.read_text()
old = '\t\t"glm-worker/internal/workflow/workflow.go",\n\t\t"glm-worker/internal/workflow/quality_gate.go",\n'
new = '\t\t"glm-worker/internal/workflow/workflow.go",\n\t\t"glm-worker/internal/workflow/review_flow.go",\n\t\t"glm-worker/internal/workflow/quality_gate.go",\n'
if old not in s:
    raise SystemExit('fixture marker missing')
p.write_text(s.replace(old, new, 1))
