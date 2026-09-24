from pathlib import Path


def replace_between(path: str, start_marker: str, end_marker: str, replacement: str) -> None:
    p = Path(path)
    text = p.read_text()
    start = text.find(start_marker)
    if start < 0:
        raise SystemExit(f"{path}: start marker not found")
    end = text.find(end_marker, start)
    if end < 0:
        raise SystemExit(f"{path}: end marker not found")
    p.write_text(text[:start] + replacement + text[end:])


replace_between(
    "scripts/manage-pull-hook.sh",
    'install_lock_path="$common_dir/codex-worker-orchestrator/hooks-install.lock"\n',
    "trap 'release_install_lock' EXIT\n",
    r'''install_lock_root="$common_dir/codex-worker-orchestrator/hooks-install.lock"
install_lock_ticket="$install_lock_root/$$"
mkdir -p "$install_lock_root"
lock_owned=0
release_install_lock() {
	if [ "$lock_owned" -eq 1 ]; then
		rmdir "$install_lock_ticket" 2>/dev/null || true
		lock_owned=0
	fi
}
acquire_install_lock() {
	if [ -e "$install_lock_ticket" ] || [ -L "$install_lock_ticket" ]; then
		if [ ! -d "$install_lock_ticket" ] || [ -L "$install_lock_ticket" ] || ! rmdir "$install_lock_ticket" 2>/dev/null; then
			printf 'git hook: own stale hook activation ticket is invalid: %s\n' "$install_lock_ticket" >&2
			return 1
		fi
	fi
	if ! mkdir "$install_lock_ticket" 2>/dev/null; then
		printf 'git hook: failed to create hook activation ticket: %s\n' "$install_lock_ticket" >&2
		return 1
	fi
	lock_owned=1
	for ticket in "$install_lock_root"/*; do
		if [ ! -e "$ticket" ] && [ ! -L "$ticket" ]; then
			continue
		fi
		if [ "$ticket" = "$install_lock_ticket" ]; then
			continue
		fi
		if [ ! -d "$ticket" ] || [ -L "$ticket" ]; then
			printf 'git hook: hook activation ticket is invalid: %s\n' "$ticket" >&2
			release_install_lock
			return 1
		fi
		owner_pid=${ticket##*/}
		case "$owner_pid" in
		'' | *[!0-9]*)
			printf 'git hook: hook activation ticket owner is invalid: %s\n' "$ticket" >&2
			release_install_lock
			return 1
			;;
		esac
		if kill -0 "$owner_pid" 2>/dev/null; then
			printf 'git hook: another installer owns hook activation: %s\n' "$ticket" >&2
			release_install_lock
			return 1
		fi
	done
	return 0
}
if ! acquire_install_lock; then
	exit 1
fi
''',
)

replace_between(
    "tests/install_hook_ownership_smoke.sh",
    'lock="$common/codex-worker-orchestrator/hooks-install.lock"\n',
    'test ! -e "$lock"\n',
    r'''lock_root="$common/codex-worker-orchestrator/hooks-install.lock"
mkdir -p "$lock_root"
live_ticket="$lock_root/$$"
mkdir "$live_ticket"
if sh "$helper" install "$repo" "$guard_bin" >"$tmp/install-lock.stdout" 2>"$tmp/install-lock.stderr"; then
	printf '%s\n' 'concurrent hook installer unexpectedly bypassed activation lock' >&2
	exit 1
fi
grep -Fq 'another installer owns hook activation' "$tmp/install-lock.stderr"
rmdir "$live_ticket"
stale_ticket="$lock_root/99999999"
mkdir "$stale_ticket"
sh "$helper" install "$repo" "$guard_bin"
assert_managed_hooks "$repo"
test -d "$stale_ticket"
''',
)

p = Path("tests/install_hook_ownership_smoke.sh")
text = p.read_text()
old = 'test ! -e "$lock"\n'
if text.count(old) != 1:
    raise SystemExit("old lock assertion replacement mismatch")
p.write_text(text.replace(old, "", 1))
