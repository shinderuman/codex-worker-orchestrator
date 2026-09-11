from pathlib import Path

path = Path("install.sh")
text = path.read_text()
anchor = '''copy_tree() {
\tsrc=$1
\tdst=$2
\tmkdir -p "$dst"
\trsync -a "$src/" "$dst/"
}

install_codex_files() {
'''
replacement = '''copy_tree() {
\tsrc=$1
\tdst=$2
\tmkdir -p "$dst"
\trsync -a "$src/" "$dst/"
}

legacy_global_agents_matches_repository() {
\ttarget=$1
\t[ -f "$target" ] || return 1
\tif cmp -s "$repo_root/codex/AGENTS.md" "$target"; then
\t\treturn 0
\tfi
\thistory=$(mktemp "${TMPDIR:-/tmp}/codex-agents-history.XXXXXX")
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
\treturn 1
}

install_codex_files() {
'''
if anchor not in text:
    raise SystemExit("install helper anchor missing")
text = text.replace(anchor, replacement, 1)
old = '''\t\tif [ "$relative_path" = 'AGENTS.md' ]; then
\t\t\tcontinue
\t\tfi
'''
new = '''\t\tif [ "$relative_path" = 'AGENTS.md' ]; then
\t\t\ttarget="$codex_dir/AGENTS.md"
\t\t\tif legacy_global_agents_matches_repository "$target"; then
\t\t\t\trm -f "$target"
\t\t\t\tprintf 'removed legacy managed: %s\\n' "$target"
\t\t\tfi
\t\t\tcontinue
\t\tfi
'''
if old not in text:
    raise SystemExit("legacy manifest anchor missing")
path.write_text(text.replace(old, new, 1))

path = Path("tests/install_smoke.sh")
text = path.read_text()
anchor = '''if grep -Fxq 'AGENTS.md' "$home/.codex/.codex-config-managed-files"; then
\tprintf '%s\\n' 'user-global AGENTS.md remains installer-managed' >&2
\texit 1
fi
'''
addition = '''if grep -Fxq 'AGENTS.md' "$home/.codex/.codex-config-managed-files"; then
\tprintf '%s\\n' 'user-global AGENTS.md remains installer-managed' >&2
\texit 1
fi
cp "$repo/codex/AGENTS.md" "$home/.codex/AGENTS.md"
printf '%s\\n' 'AGENTS.md' >>"$home/.codex/.codex-config-managed-files"
run_install
test ! -e "$home/.codex/AGENTS.md"
if grep -Fxq 'AGENTS.md' "$home/.codex/.codex-config-managed-files"; then
\tprintf '%s\\n' 'legacy global AGENTS.md remains installer-managed' >&2
\texit 1
fi
'''
if anchor not in text:
    raise SystemExit("smoke legacy manifest anchor missing")
path.write_text(text.replace(anchor, addition, 1))
