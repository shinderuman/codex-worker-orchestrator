from pathlib import Path

path = Path("glm-worker/internal/packet/result_test.go")
text = path.read_text()
old = '''\t\t\twantKeys := map[string]bool{"status": true, "risk": true, "targets": true, "artifacts": true}\n\t\t\tfor _, field := range contract {\n\t\t\t\twantKeys[field.machine] = true\n\t\t\t}\n'''
new = '''\t\t\twantKeys := map[string]bool{"status": true, "risk": true, "targets": true, "artifacts": true}\n\t\t\tfor _, field := range contract {\n\t\t\t\twantKeys[string(field)] = true\n\t\t\t}\n'''
if old not in text:
    raise SystemExit("result_test machine key target missing")
path.write_text(text.replace(old, new, 1))
