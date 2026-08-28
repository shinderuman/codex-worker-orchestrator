from pathlib import Path

path = Path("glm-worker/internal/runner/structured_test.go")
text = path.read_text()
old = '''\tassertStatusEnum(t, reviewerSchema, []string{"PASS", "FIX_REQUIRED", "NEEDS_SOL_REVIEW"})
'''
new = '''\tassertStatusEnum(t, reviewerSchema, []string{"PASS", "FIX_REQUIRED", "NEEDS_SOL_REVIEW", "NEEDS_SOL_DECISION"})
'''
if text.count(old) != 1:
    raise SystemExit(f"reviewer runner schema anchor count {text.count(old)} != 1")
path.write_text(text.replace(old, new, 1))
