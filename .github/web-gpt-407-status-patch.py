from pathlib import Path

path = Path("glm-worker/internal/codexcontext/context.go")
text = path.read_text()
old = '''\tagentsState, err := projectAgentsOverrideState(root)\n\tif err != nil {\n\t\treturn Result{}, err\n\t}\n\tif agentsState == "conflict" || (state == "enabled") != (agentsState == "enabled") {\n\t\tstate = "conflict"\n\t\tdetail = "project config and project-scoped AGENTS bootstrap ownership do not match"\n\t}\n'''
new = '''\tagentsState, err := projectAgentsOverrideState(root)\n\tif err != nil {\n\t\treturn Result{}, err\n\t}\n\tagentsIgnored := false\n\tif agentsState == "enabled" {\n\t\tagentsIgnored, err = projectAgentsIgnored(root)\n\t\tif err != nil {\n\t\t\treturn Result{}, err\n\t\t}\n\t}\n\tif agentsState == "conflict" || (state == "enabled") != (agentsState == "enabled") || (agentsState == "enabled" && !agentsIgnored) {\n\t\tstate = "conflict"\n\t\tdetail = "project config and project-scoped AGENTS bootstrap ownership do not match"\n\t}\n'''
if text.count(old) != 1:
    raise SystemExit(f"status anchor count={text.count(old)}")
path.write_text(text.replace(old, new, 1))
Path(".github/web-gpt-407-status-patch.py").unlink()
Path(".github/workflows/web-gpt-407-status-patch.yml").unlink()
