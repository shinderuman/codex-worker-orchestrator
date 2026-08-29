from pathlib import Path

p = Path("glm-worker/internal/runner/operation_category.go")
s = p.read_text()
s = s.replace('var shellProgramCategories = map[string]string{', 'const bashToolName = "Bash"\n\nvar shellProgramCategories = map[string]string{', 1)
s = s.replace('case "Bash":', 'case bashToolName:', 1)
p.write_text(s)

p = Path("glm-worker/internal/runner/validation_observation.go")
s = p.read_text().replace('if toolName != "Bash" || len(input) == 0 {', 'if toolName != bashToolName || len(input) == 0 {', 1)
p.write_text(s)

p = Path("glm-worker/internal/state/events.go")
s = p.read_text()
block = '''const (
\tValidationResultPass    = "pass"
\tValidationResultFail    = "fail"
\tValidationResultUnknown = "unknown"
)

'''
if block not in s:
    raise SystemExit("validation result const block not found")
s = s.replace(block, "", 1)
marker = 'const taskEventLogVersion = 1\n'
if marker not in s:
    raise SystemExit("task event const marker not found")
s = s.replace(marker, block + marker, 1)
p.write_text(s)
