from pathlib import Path

path = Path("install.sh")
text = path.read_text()
old = '''\thistory=$(mktemp "${TMPDIR:-/tmp}/codex-agents-history.XXXXXX")
\tif ! git -C "$repo_root" log --format=%H -- codex/AGENTS.md >"$history"; then
\t\trm -f "$history"
\t\treturn 1
\tfi
\twhile IFS= read -r revision; do
\t\t[ -n "$revision" ] || continue
\t\tif git -C "$repo_root" show "$revision:codex/AGENTS.md" 2>/dev/null | cmp -s - "$target"; then
\t\t\trm -f "$history"
\t\t\treturn 0
\t\tfi
\tdone <"$history"
\trm -f "$history"
'''
new = '''\tfor revision in $(git -C "$repo_root" log --format=%H -- codex/AGENTS.md); do
\t\tif git -C "$repo_root" show "$revision:codex/AGENTS.md" 2>/dev/null | cmp -s - "$target"; then
\t\t\treturn 0
\t\tfi
\tdone
'''
if old not in text:
    raise SystemExit("legacy history anchor missing")
path.write_text(text.replace(old, new, 1))
