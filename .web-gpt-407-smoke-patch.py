from pathlib import Path

path = Path("tests/install_smoke.sh")
text = path.read_text()
old = '''test -f "$repo/AGENTS.override.md"
grep -Fq "$home/.codex/instructions/codex-worker-orchestrator.md" "$repo/AGENTS.override.md"
grep -Fq 'managed-by:' "$repo/AGENTS.override.md"
git -C "$repo" check-ignore -q -- .codex/config.toml
git -C "$repo" check-ignore -q -- AGENTS.override.md
if git -C "$repo" status --porcelain --untracked-files=all | grep -Eq '(.codex/config.toml|AGENTS.override.md)'; then
'''
new = '''grep -Fq 'developer_instructions = ' "$repo/.codex/config.toml"
grep -Fq 'instructions/codex-worker-orchestrator.md' "$repo/.codex/config.toml"
test ! -e "$repo/AGENTS.override.md"
git -C "$repo" check-ignore -q -- .codex/config.toml
if git -C "$repo" status --porcelain --untracked-files=all | grep -Fq '.codex/config.toml'; then
'''
if old not in text:
    raise SystemExit("install smoke project bootstrap anchor missing")
path.write_text(text.replace(old, new, 1))
