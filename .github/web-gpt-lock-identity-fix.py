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
    'install_lock_root="$common_dir/codex-worker-orchestrator/hooks-install.lock"\n',
    "trap 'release_install_lock' EXIT\n",
    r'''install_lock_root="$common_dir/codex-worker-orchestrator/hooks-install.lock"
install_lock_ticket="$install_lock_root/$$"
install_lock_candidate="$install_lock_root/.candidate.$$"
mkdir -p "$install_lock_root"
lock_owned=0
remove_own_lock_dir() {
	path=$1
	if [ ! -d "$path" ] || [ -L "$path" ]; then
		return 1
	fi
	if [ -e "$path/started" ] || [ -L "$path/started" ]; then
		if [ ! -f "$path/started" ] || [ -L "$path/started" ]; then
			return 1
		fi
		rm -f "$path/started"
	fi
	rmdir "$path"
}
release_install_lock() {
	if [ "$lock_owned" -eq 1 ]; then
		remove_own_lock_dir "$install_lock_ticket" 2>/dev/null || true
		lock_owned=0
	fi
}
acquire_install_lock() {
	owner_started=$(LC_ALL=C ps -p "$$" -o lstart= 2>/dev/null || true)
	if [ -z "$owner_started" ]; then
		printf '%s\n' 'git hook: cannot resolve hook installer process identity' >&2
		return 1
	fi
	for own_path in "$install_lock_candidate" "$install_lock_ticket"; do
		if [ -e "$own_path" ] || [ -L "$own_path" ]; then
			if ! remove_own_lock_dir "$own_path"; then
				printf 'git hook: own stale hook activation ticket is invalid: %s\n' "$own_path" >&2
				return 1
			fi
		fi
	done
	if ! mkdir "$install_lock_candidate" 2>/dev/null; then
		printf 'git hook: failed to create hook activation candidate: %s\n' "$install_lock_candidate" >&2
		return 1
	fi
	if ! printf '%s\n' "$owner_started" >"$install_lock_candidate/started"; then
		remove_own_lock_dir "$install_lock_candidate" 2>/dev/null || true
		return 1
	fi
	if ! mv "$install_lock_candidate" "$install_lock_ticket" 2>/dev/null; then
		remove_own_lock_dir "$install_lock_candidate" 2>/dev/null || true
		printf 'git hook: failed to publish hook activation ticket: %s\n' "$install_lock_ticket" >&2
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
		if [ ! -f "$ticket/started" ] || [ -L "$ticket/started" ]; then
			printf 'git hook: hook activation ticket identity is invalid: %s\n' "$ticket" >&2
			release_install_lock
			return 1
		fi
		ticket_started=$(cat "$ticket/started")
		if kill -0 "$owner_pid" 2>/dev/null; then
			current_started=$(LC_ALL=C ps -p "$owner_pid" -o lstart= 2>/dev/null || true)
			if [ -z "$current_started" ]; then
				printf 'git hook: cannot verify live hook activation owner: %s\n' "$ticket" >&2
				release_install_lock
				return 1
			fi
			if [ "$current_started" = "$ticket_started" ]; then
				printf 'git hook: another installer owns hook activation: %s\n' "$ticket" >&2
				release_install_lock
				return 1
			fi
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
    'lock_root="$common/codex-worker-orchestrator/hooks-install.lock"\n',
    'test -d "$stale_ticket"\n',
    r'''lock_root="$common/codex-worker-orchestrator/hooks-install.lock"
mkdir -p "$lock_root"
live_ticket="$lock_root/$$"
mkdir "$live_ticket"
live_started=$(LC_ALL=C ps -p "$$" -o lstart=)
printf '%s\n' "$live_started" >"$live_ticket/started"
if sh "$helper" install "$repo" "$guard_bin" >"$tmp/install-lock.stdout" 2>"$tmp/install-lock.stderr"; then
	printf '%s\n' 'concurrent hook installer unexpectedly bypassed activation lock' >&2
	exit 1
fi
grep -Fq 'another installer owns hook activation' "$tmp/install-lock.stderr"
rm -f "$live_ticket/started"
rmdir "$live_ticket"
stale_ticket="$lock_root/$$"
mkdir "$stale_ticket"
printf '%s\n' 'stale-process-start' >"$stale_ticket/started"
sh "$helper" install "$repo" "$guard_bin"
assert_managed_hooks "$repo"
test -d "$stale_ticket"
test "$(cat "$stale_ticket/started")" = 'stale-process-start'
''',
)
