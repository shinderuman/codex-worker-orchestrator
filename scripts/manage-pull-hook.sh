#!/bin/sh
set -eu

mode=${1:-}
repo_root=${2:-}
legacy_hooks_path=.githooks
required_hooks='post-merge pre-commit reference-transaction pre-push'
legacy_managed_state='version=1 baseline=absent value=.githooks'
legacy_pending_state='version=1 baseline=absent pending=.githooks'

if [ "$mode" != install ] && [ "$mode" != retire ]; then
	printf '%s\n' 'usage: manage-pull-hook.sh <install|retire> <repository>' >&2
	exit 2
fi
if [ -z "$repo_root" ]; then
	printf '%s\n' 'repository path is required' >&2
	exit 2
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
adopted_state="version=2 baseline=$legacy_hooks_path value=$managed_hooks_path"
adopted_pending_state="version=2 baseline=$legacy_hooks_path pending=$managed_hooks_path"
legacy_migration_pending_state="version=2 baseline=absent pending=$managed_hooks_path source=$legacy_hooks_path"

state_path=$(git -C "$repo_root" rev-parse --git-path codex-worker-orchestrator/hooks-path.state)
case "$state_path" in
/*) ;;
*) state_path="$repo_root/$state_path" ;;
esac

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
	"$adopted_state") state_kind=adopted ;;
	"$adopted_pending_state") state_kind=adopted-pending ;;
	"$legacy_migration_pending_state") state_kind=legacy-migration-pending ;;
	"$legacy_managed_state") state_kind=legacy-managed ;;
	"$legacy_pending_state") state_kind=legacy-pending ;;
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

write_state() {
	value=$1
	state_dir=${state_path%/*}
	mkdir -p "$state_dir"
	tmp_state="$state_path.tmp.$$"
	trap 'rm -f "$tmp_state"' EXIT HUP INT TERM
	umask 077
	printf '%s\n' "$value" >"$tmp_state"
	mv "$tmp_state" "$state_path"
	trap - EXIT HUP INT TERM
}

managed_path_unclaimed() {
	if [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
		printf 'git hook: managed snapshot path exists without installer ownership state: %s\n' "$managed_hooks_path" >&2
		return 1
	fi
	return 0
}

install_managed_hooks() {
	staging_hooks_path="$managed_hooks_path.stage.$$"
	backup_hooks_path="$managed_hooks_path.backup.$$"
	cleanup_snapshot_work() {
		rm -rf "$staging_hooks_path" "$backup_hooks_path"
	}
	trap 'cleanup_snapshot_work' EXIT HUP INT TERM
	rm -rf "$staging_hooks_path" "$backup_hooks_path"
	mkdir -p "$staging_hooks_path"

	for hook in $required_hooks; do
		staged_hook="$staging_hooks_path/$hook"
		if ! git -C "$repo_root" show "HEAD:.githooks/$hook" >"$staged_hook"; then
			printf 'git hook: committed source missing: .githooks/%s\n' "$hook" >&2
			cleanup_snapshot_work
			exit 1
		fi
		chmod 755 "$staged_hook"
		if [ ! -s "$staged_hook" ] || [ ! -x "$staged_hook" ]; then
			printf 'git hook: staged snapshot is not executable and non-empty: .githooks/%s\n' "$hook" >&2
			cleanup_snapshot_work
			exit 1
		fi
	done

	had_active=0
	if [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
		if [ ! -d "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
			printf 'git hook: managed snapshot path is not an installer-owned directory: %s\n' "$managed_hooks_path" >&2
			cleanup_snapshot_work
			exit 1
		fi
		if ! mv "$managed_hooks_path" "$backup_hooks_path"; then
			printf '%s\n' 'git hook: failed to stage existing managed snapshot for replacement' >&2
			cleanup_snapshot_work
			exit 1
		fi
		had_active=1
	fi

	if ! mv "$staging_hooks_path" "$managed_hooks_path"; then
		printf '%s\n' 'git hook: failed to activate validated managed snapshot' >&2
		if [ "$had_active" -eq 1 ] && [ ! -e "$managed_hooks_path" ] && [ ! -L "$managed_hooks_path" ]; then
			if ! mv "$backup_hooks_path" "$managed_hooks_path"; then
				printf 'git hook: failed to restore previous managed snapshot; backup retained at %s\n' "$backup_hooks_path" >&2
				trap - EXIT HUP INT TERM
				exit 1
			fi
		fi
		cleanup_snapshot_work
		exit 1
	fi

	rm -rf "$backup_hooks_path"
	trap - EXIT HUP INT TERM
}

remove_managed_hooks() {
	rm -rf "$managed_hooks_path"
}

verify_managed_install() {
	expected_state=$1
	if [ ! -f "$state_path" ] || [ -L "$state_path" ] || [ "$(cat "$state_path")" != "$expected_state" ]; then
		printf '%s\n' 'git hook: managed ownership state postcondition failed' >&2
		exit 1
	fi
	if ! current_hooks_path=$(git -C "$repo_root" config --local --get-all core.hooksPath); then
		printf '%s\n' 'git hook: managed core.hooksPath postcondition is missing' >&2
		exit 1
	fi
	if [ "$current_hooks_path" != "$managed_hooks_path" ]; then
		printf 'git hook: managed core.hooksPath postcondition mismatch: %s\n' "$current_hooks_path" >&2
		exit 1
	fi
	for hook in $required_hooks; do
		active_hook="$managed_hooks_path/$hook"
		if [ ! -f "$active_hook" ] || [ -L "$active_hook" ] || [ ! -s "$active_hook" ] || [ ! -x "$active_hook" ]; then
			printf 'git hook: managed snapshot postcondition failed: %s\n' "$active_hook" >&2
			exit 1
		fi
		if ! git -C "$repo_root" show "HEAD:.githooks/$hook" | cmp -s - "$active_hook"; then
			printf 'git hook: managed snapshot differs from committed source: .githooks/%s\n' "$hook" >&2
			exit 1
		fi
	done
}

legacy_tracked_hooks_adoptable() {
	if [ -e "$managed_hooks_path" ] || [ -L "$managed_hooks_path" ]; then
		return 1
	fi
	for hook in $required_hooks; do
		tracked_hook="$repo_root/$legacy_hooks_path/$hook"
		if [ ! -f "$tracked_hook" ] || [ -L "$tracked_hook" ] || [ ! -s "$tracked_hook" ] || [ ! -x "$tracked_hook" ]; then
			return 1
		fi
		tracked_mode=$(git -C "$repo_root" ls-tree HEAD -- "$legacy_hooks_path/$hook" | awk 'NR == 1 { print $1 }')
		if [ "$tracked_mode" != 100755 ]; then
			return 1
		fi
		if ! git -C "$repo_root" show "HEAD:$legacy_hooks_path/$hook" | cmp -s - "$tracked_hook"; then
			return 1
		fi
	done
	return 0
}

restore_legacy_hooks_path() {
	if ! git -C "$repo_root" config --local core.hooksPath "$legacy_hooks_path"; then
		printf 'git hook: failed to restore legacy core.hooksPath: %s\n' "$legacy_hooks_path" >&2
		exit 1
	fi
}

if [ "$mode" = retire ]; then
	if [ "$state_present" -eq 0 ]; then
		printf '%s\n' 'git hook: retire unchanged: core.hooksPath is not installer-owned'
		exit 0
	fi

	case "$state_kind" in
	adopted | adopted-pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
			restore_legacy_hooks_path
			printf '%s\n' 'git hook: retired installer ownership and restored preexisting .githooks'
		elif [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$legacy_hooks_path" ]; then
			printf '%s\n' 'git hook: retire kept already-restored preexisting .githooks'
		elif [ "$hooks_path_present" -eq 1 ]; then
			printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
		else
			printf '%s\n' 'git hook: retire preserved externally unset core.hooksPath'
		fi
		;;
	legacy-managed | legacy-pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$legacy_hooks_path" ]; then
			git -C "$repo_root" config --local --unset-all core.hooksPath
			printf '%s\n' 'git hook: retired legacy installer-owned core.hooksPath'
		elif [ "$hooks_path_present" -eq 1 ]; then
			printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
		else
			printf '%s\n' 'git hook: retire cleared pending legacy ownership state'
		fi
		;;
	legacy-migration-pending)
		if [ "$hooks_path_present" -eq 1 ] && { [ "$hooks_path" = "$legacy_hooks_path" ] || [ "$hooks_path" = "$managed_hooks_path" ]; }; then
			git -C "$repo_root" config --local --unset-all core.hooksPath
			printf '%s\n' 'git hook: retired interrupted legacy migration to absent baseline'
		elif [ "$hooks_path_present" -eq 1 ]; then
			printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
		else
			printf '%s\n' 'git hook: retire cleared interrupted legacy migration state'
		fi
		;;
	managed | pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
			git -C "$repo_root" config --local --unset-all core.hooksPath
			printf '%s\n' 'git hook: retired installer-owned core.hooksPath'
		elif [ "$hooks_path_present" -eq 1 ]; then
			printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
		else
			printf '%s\n' 'git hook: retire cleared pending ownership state'
		fi
		;;
	esac
	remove_managed_hooks
	rm -f "$state_path"
	exit 0
fi

if [ "$state_present" -eq 1 ]; then
	case "$state_kind" in
	managed | adopted)
		if [ "$hooks_path_present" -ne 1 ] || [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: installer ownership state conflicts with current core.hooksPath: %s\n' "${hooks_path:-<unset>}" >&2
			exit 1
		fi
		install_managed_hooks
		case "$state_kind" in
		managed) verify_managed_install "$managed_state" ;;
		adopted) verify_managed_install "$adopted_state" ;;
		esac
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
		fi
		write_state "$managed_state"
		verify_managed_install "$managed_state"
		printf '%s\n' 'git hook: recovered interrupted installer-owned snapshot hooks activation'
		exit 0
		;;
	adopted-pending)
		if [ "$hooks_path_present" -eq 0 ]; then
			printf '%s\n' 'git hook: pending legacy adoption preserves externally unset core.hooksPath' >&2
			exit 1
		fi
		if [ "$hooks_path" != "$legacy_hooks_path" ] && [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: pending legacy adoption preserves externally changed core.hooksPath: %s\n' "$hooks_path" >&2
			exit 1
		fi
		install_managed_hooks
		if [ "$hooks_path" != "$managed_hooks_path" ]; then
			if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
				printf '%s\n' 'git hook: failed to complete preexisting .githooks adoption; retry install to continue' >&2
				exit 1
			fi
		fi
		write_state "$adopted_state"
		verify_managed_install "$adopted_state"
		printf '%s\n' 'git hook: completed preexisting .githooks adoption'
		exit 0
		;;
	legacy-migration-pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" != "$legacy_hooks_path" ] && [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: pending legacy migration preserves externally changed core.hooksPath: %s\n' "$hooks_path" >&2
			exit 1
		fi
		install_managed_hooks
		if [ "$hooks_path_present" -eq 0 ] || [ "$hooks_path" != "$managed_hooks_path" ]; then
			if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
				printf '%s\n' 'git hook: failed to recover legacy installer-owned hook migration' >&2
				exit 1
			fi
		fi
		write_state "$managed_state"
		verify_managed_install "$managed_state"
		printf '%s\n' 'git hook: recovered legacy installer-owned hook migration'
		exit 0
		;;
	legacy-managed | legacy-pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" != "$legacy_hooks_path" ]; then
			printf 'git hook: legacy ownership state conflicts with current core.hooksPath: %s\n' "$hooks_path" >&2
			exit 1
		fi
		write_state "$legacy_migration_pending_state"
		install_managed_hooks
		if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
			printf '%s\n' 'git hook: failed to migrate legacy installer-owned snapshot hooks; retry install to continue' >&2
			exit 1
		fi
		write_state "$managed_state"
		verify_managed_install "$managed_state"
		printf '%s\n' 'git hook: migrated legacy installer-owned .githooks to snapshot hooks'
		exit 0
		;;
	esac
fi

if [ "$hooks_path_present" -eq 1 ]; then
	if [ "$hooks_path" = "$legacy_hooks_path" ] && legacy_tracked_hooks_adoptable; then
		write_state "$adopted_pending_state"
		install_managed_hooks
		if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
			printf '%s\n' 'git hook: failed to adopt preexisting tracked .githooks; retry install to continue' >&2
			exit 1
		fi
		write_state "$adopted_state"
		verify_managed_install "$adopted_state"
		printf '%s\n' 'git hook: adopted preexisting tracked .githooks into installer-owned snapshot hooks'
		exit 0
	fi
	if [ "$hooks_path" = "$legacy_hooks_path" ]; then
		printf '%s\n' 'git hook: preexisting .githooks cannot be safely adopted; preserving external ownership' >&2
	else
		printf 'git hook: preexisting core.hooksPath cannot be replaced safely: %s\n' "${hooks_path:-<empty>}" >&2
	fi
	exit 1
fi

managed_path_unclaimed
write_state "$pending_state"
install_managed_hooks
if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
	printf '%s\n' 'git hook: failed to enable managed snapshot hooks; pending ownership state retained for recovery' >&2
	exit 1
fi
write_state "$managed_state"
verify_managed_install "$managed_state"
printf '%s\n' 'git hook: enabled installer-owned snapshot hooks'
