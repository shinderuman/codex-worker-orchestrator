#!/bin/sh
set -eu

mode=${1:-}
repo_root=${2:-}
glm_parent_action_path=${3:-}
required_hooks='post-merge pre-commit reference-transaction pre-push'

if [ "$mode" != install ] && [ "$mode" != retire ]; then
	printf '%s\n' 'usage: manage-pull-hook.sh <install|retire> <repository>' >&2
	exit 2
fi
if [ -z "$repo_root" ]; then
	printf '%s\n' 'repository path is required' >&2
	exit 2
fi
if [ "$mode" = install ] && [ -z "$glm_parent_action_path" ]; then
	glm_parent_action_path=$(command -v glm-parent-action || true)
fi
if [ "$mode" = install ]; then
	case "$glm_parent_action_path" in
	/*) ;;
	*)
		printf '%s\n' 'git hook: absolute glm-parent-action path is required' >&2
		exit 1
		;;
	esac
	if [ ! -x "$glm_parent_action_path" ]; then
		printf 'git hook: glm-parent-action is not executable: %s\n' "$glm_parent_action_path" >&2
		exit 1
	fi
fi

unset GIT_DIR GIT_WORK_TREE GIT_COMMON_DIR GIT_INDEX_FILE GIT_OBJECT_DIRECTORY GIT_ALTERNATE_OBJECT_DIRECTORIES

if ! git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
	printf '%s\n' 'git hook: skipped: not a Git repository'
	exit 0
fi

common_dir=$(git -C "$repo_root" rev-parse --git-common-dir)
case "$common_dir" in
/*) ;;
*) common_dir="$repo_root/$common_dir" ;;
esac
managed_hooks_path="$common_dir/codex-worker-orchestrator/hooks"
managed_state="version=2 baseline=absent value=$managed_hooks_path"
pending_state="version=2 baseline=absent pending=$managed_hooks_path"
install_lock_root="$common_dir/codex-worker-orchestrator/hooks-install.lock"
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
trap 'release_install_lock' EXIT
trap 'release_install_lock; exit 129' HUP
trap 'release_install_lock; exit 130' INT
trap 'release_install_lock; exit 143' TERM

state_path="$common_dir/codex-worker-orchestrator/hooks-path.state"

state_present=0
state_kind=
if [ -e "$state_path" ] || [ -L "$state_path" ]; then
	if [ ! -f "$state_path" ] || [ -L "$state_path" ]; then
		printf 'git hook ownership state is not a regular file: %s\n' "$state_path" >&2
		exit 1
	fi
	case "$(cat "$state_path")" in
	"$managed_state") state_kind=managed ;;
	"$pending_state") state_kind=pending ;;
	*)
		printf 'git hook ownership state is invalid: %s\n' "$state_path" >&2
		exit 1
		;;
	esac
	state_present=1
fi

hooks_path_present=0
hooks_path=
if hooks_path=$(git -C "$repo_root" config --local --get-all core.hooksPath); then
	hooks_path_present=1
else
	status=$?
	if [ "$status" -ne 1 ]; then
		printf '%s\n' 'git hook: failed to read repository core.hooksPath' >&2
		exit "$status"
	fi
fi

initial_state_present=$state_present
initial_state_value=
if [ "$state_present" -eq 1 ]; then
	initial_state_value=$(cat "$state_path")
fi
initial_hooks_path_present=$hooks_path_present
initial_hooks_path=$hooks_path
hooks_path_written=0

write_state() {
	value=$1
	state_dir=${state_path%/*}
	mkdir -p "$state_dir"
	tmp_state="$state_path.tmp.$$"
	umask 077
	if ! printf '%s\n' "$value" >"$tmp_state"; then
		rm -f "$tmp_state"
		return 1
	fi
	if ! mv "$tmp_state" "$state_path"; then
		rm -f "$tmp_state"
		return 1
	fi
}

managed_path_unclaimed() {
	if [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
		printf 'git hook: managed snapshot path exists without installer ownership state: %s\n' "$managed_hooks_path" >&2
		return 1
	fi
	return 0
}

staging_hooks_path=
backup_hooks_path=
cleanup_snapshot_work() {
	if [ -n "$backup_hooks_path" ] && { [ -e "$backup_hooks_path" ] || [ -L "$backup_hooks_path" ]; }; then
		rm -rf "$managed_hooks_path"
		if ! mv "$backup_hooks_path" "$managed_hooks_path"; then
			printf 'git hook: failed to restore previous managed snapshot; backup retained at %s\n' "$backup_hooks_path" >&2
			return 1
		fi
	fi
	if [ -n "$staging_hooks_path" ]; then
		rm -rf "$staging_hooks_path"
	fi
}

restore_install_baseline() {
	if [ -n "$backup_hooks_path" ] && { [ -e "$backup_hooks_path" ] || [ -L "$backup_hooks_path" ]; }; then
		rm -rf "$managed_hooks_path"
		if ! mv "$backup_hooks_path" "$managed_hooks_path"; then
			printf 'git hook: failed to restore previous managed snapshot; backup retained at %s\n' "$backup_hooks_path" >&2
			return 1
		fi
	else
		rm -rf "$managed_hooks_path"
	fi
	if [ -n "$staging_hooks_path" ]; then
		rm -rf "$staging_hooks_path"
	fi
	staging_hooks_path=
	backup_hooks_path=
	if [ "$hooks_path_written" -eq 1 ]; then
		current_hooks_path=
		if current_hooks_path=$(git -C "$repo_root" config --local --get-all core.hooksPath); then
			current_hooks_path_present=1
		else
			status=$?
			if [ "$status" -ne 1 ]; then
				printf '%s\n' 'git hook: failed to inspect core.hooksPath before rollback' >&2
				return 1
			fi
			current_hooks_path_present=0
		fi
		if [ "$current_hooks_path_present" -ne 1 ] || [ "$current_hooks_path" != "$managed_hooks_path" ]; then
			printf '%s\n' 'git hook: rollback preserved externally changed core.hooksPath' >&2
			return 1
		fi
		if [ "$initial_hooks_path_present" -eq 1 ]; then
			if ! git -C "$repo_root" config --local core.hooksPath "$initial_hooks_path"; then
				printf '%s\n' 'git hook: failed to restore previous core.hooksPath' >&2
				return 1
			fi
		else
			git -C "$repo_root" config --local --unset-all core.hooksPath >/dev/null 2>&1 || true
			if git -C "$repo_root" config --local --get-all core.hooksPath >/dev/null 2>&1; then
				printf '%s\n' 'git hook: failed to restore absent core.hooksPath baseline' >&2
				return 1
			fi
		fi
	fi
	if [ "$initial_state_present" -eq 1 ]; then
		if ! write_state "$initial_state_value"; then
			printf '%s\n' 'git hook: failed to restore previous ownership state' >&2
			return 1
		fi
	else
		rm -f "$state_path"
	fi
	trap 'release_install_lock' EXIT
	trap 'release_install_lock; exit 129' HUP
	trap 'release_install_lock; exit 130' INT
	trap 'release_install_lock; exit 143' TERM
	return 0
}

verify_or_rollback() {
	expected_state=$1
	if verify_managed_install "$expected_state"; then
		return 0
	fi
	if ! restore_install_baseline; then
		printf '%s\n' 'git hook: failed to restore install baseline after verification failure' >&2
	fi
	return 1
}

finalize_managed_hooks() {
	rm -rf "$staging_hooks_path" "$backup_hooks_path"
	staging_hooks_path=
	backup_hooks_path=
	trap 'release_install_lock' EXIT
	trap 'release_install_lock; exit 129' HUP
	trap 'release_install_lock; exit 130' INT
	trap 'release_install_lock; exit 143' TERM
}

install_managed_hooks() {
	staging_hooks_path="$managed_hooks_path.stage.$$"
	backup_hooks_path="$managed_hooks_path.backup.$$"
	trap 'cleanup_snapshot_work; release_install_lock' EXIT
	trap 'cleanup_snapshot_work; release_install_lock; exit 129' HUP
	trap 'cleanup_snapshot_work; release_install_lock; exit 130' INT
	trap 'cleanup_snapshot_work; release_install_lock; exit 143' TERM
	rm -rf "$staging_hooks_path" "$backup_hooks_path"
	mkdir -p "$staging_hooks_path"

	printf '%s\n' "$glm_parent_action_path" >"$staging_hooks_path/glm-parent-action.path"
	chmod 600 "$staging_hooks_path/glm-parent-action.path"

	for hook in $required_hooks; do
		staged_hook="$staging_hooks_path/$hook"
		if ! git -C "$repo_root" show "HEAD:.githooks/$hook" >"$staged_hook"; then
			printf 'git hook: committed source missing: .githooks/%s\n' "$hook" >&2
			exit 1
		fi
		chmod 755 "$staged_hook"
		if [ ! -s "$staged_hook" ] || [ ! -x "$staged_hook" ]; then
			printf 'git hook: staged snapshot is not executable and non-empty: .githooks/%s\n' "$hook" >&2
			exit 1
		fi
	done

	if [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
		if [ ! -d "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
			printf 'git hook: managed snapshot path is not an installer-owned directory: %s\n' "$managed_hooks_path" >&2
			exit 1
		fi
		if ! mv "$managed_hooks_path" "$backup_hooks_path"; then
			printf '%s\n' 'git hook: failed to stage existing managed snapshot for replacement' >&2
			exit 1
		fi
	fi

	if ! mv "$staging_hooks_path" "$managed_hooks_path"; then
		printf '%s\n' 'git hook: failed to activate validated managed snapshot' >&2
		exit 1
	fi
}

remove_managed_hooks() {
	rm -rf "$managed_hooks_path"
}

verify_managed_install() {
	expected_state=$1
	if [ ! -f "$state_path" ] || [ -L "$state_path" ] || [ "$(cat "$state_path")" != "$expected_state" ]; then
		printf '%s\n' 'git hook: managed ownership state postcondition failed' >&2
		return 1
	fi
	if ! current_hooks_path=$(git -C "$repo_root" config --local --get-all core.hooksPath); then
		printf '%s\n' 'git hook: managed core.hooksPath postcondition is missing' >&2
		return 1
	fi
	if [ "$current_hooks_path" != "$managed_hooks_path" ]; then
		printf 'git hook: managed core.hooksPath postcondition mismatch: %s\n' "$current_hooks_path" >&2
		return 1
	fi
	guard_path_file="$managed_hooks_path/glm-parent-action.path"
	if [ ! -f "$guard_path_file" ] || [ -L "$guard_path_file" ] || [ "$(cat "$guard_path_file")" != "$glm_parent_action_path" ]; then
		printf '%s\n' 'git hook: glm-parent-action path postcondition failed' >&2
		return 1
	fi
	for hook in $required_hooks; do
		active_hook="$managed_hooks_path/$hook"
		if [ ! -f "$active_hook" ] || [ -L "$active_hook" ] || [ ! -s "$active_hook" ] || [ ! -x "$active_hook" ]; then
			printf 'git hook: managed snapshot postcondition failed: %s\n' "$active_hook" >&2
			return 1
		fi
		if ! git -C "$repo_root" show "HEAD:.githooks/$hook" | cmp -s - "$active_hook"; then
			printf 'git hook: managed snapshot differs from committed source: .githooks/%s\n' "$hook" >&2
			return 1
		fi
	done
}

if [ "$mode" = retire ]; then
	if [ "$state_present" -eq 0 ]; then
		printf '%s\n' 'git hook: retire unchanged: core.hooksPath is not installer-owned'
		exit 0
	fi

	if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
		git -C "$repo_root" config --local --unset-all core.hooksPath
		printf '%s\n' 'git hook: retired installer-owned core.hooksPath'
	elif [ "$hooks_path_present" -eq 1 ]; then
		printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
	else
		printf '%s\n' 'git hook: retire cleared pending ownership state'
	fi
	remove_managed_hooks
	rm -f "$state_path"
	exit 0
fi

if [ "$state_present" -eq 1 ]; then
	case "$state_kind" in
	managed)
		if [ "$hooks_path_present" -ne 1 ] || [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: installer ownership state conflicts with current core.hooksPath: %s\n' "${hooks_path:-<unset>}" >&2
			exit 1
		fi
		install_managed_hooks
		if ! verify_or_rollback "$managed_state"; then
			exit 1
		fi
		finalize_managed_hooks
		printf '%s\n' 'git hook: refreshed installer-owned snapshot hooks'
		exit 0
		;;
	pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: pending installer ownership conflicts with current core.hooksPath: %s\n' "$hooks_path" >&2
			exit 1
		fi
		install_managed_hooks
		if [ "$hooks_path_present" -eq 0 ]; then
			if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
				printf '%s\n' 'git hook: failed to recover pending managed hooks activation' >&2
				exit 1
			fi
			hooks_path_written=1
		fi
		write_state "$managed_state"
		if ! verify_or_rollback "$managed_state"; then
			exit 1
		fi
		finalize_managed_hooks
		printf '%s\n' 'git hook: recovered interrupted installer-owned snapshot hooks activation'
		exit 0
		;;
	esac
fi

if [ "$hooks_path_present" -eq 1 ]; then
	printf 'git hook: preexisting core.hooksPath cannot be replaced safely: %s\n' "${hooks_path:-<empty>}" >&2
	exit 1
fi

managed_path_unclaimed
write_state "$pending_state"
install_managed_hooks
if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
	printf '%s\n' 'git hook: failed to enable managed snapshot hooks; pending ownership state retained for recovery' >&2
	exit 1
fi
hooks_path_written=1
write_state "$managed_state"
if ! verify_or_rollback "$managed_state"; then
	exit 1
fi
finalize_managed_hooks
printf '%s\n' 'git hook: enabled installer-owned snapshot hooks'
