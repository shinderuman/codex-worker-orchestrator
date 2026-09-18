#!/bin/sh
set -eu

mode=${1:-}
repo_root=${2:-}
legacy_hooks_path=.githooks
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
detached=0
if ! git -C "$repo_root" symbolic-ref -q HEAD >/dev/null 2>&1; then
	detached=1
fi

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

install_managed_hooks() {
	mkdir -p "$managed_hooks_path"
	for hook in post-merge pre-commit reference-transaction pre-push; do
		tmp_hook="$managed_hooks_path/.$hook.tmp.$$"
		trap 'rm -f "$tmp_hook"' EXIT HUP INT TERM
		if ! git -C "$repo_root" show "HEAD:.githooks/$hook" >"$tmp_hook"; then
			printf 'git hook: committed source missing: .githooks/%s\n' "$hook" >&2
			exit 1
		fi
		chmod 755 "$tmp_hook"
		mv "$tmp_hook" "$managed_hooks_path/$hook"
		trap - EXIT HUP INT TERM
	done
}

remove_managed_hooks() {
	rm -rf "$managed_hooks_path"
}

if [ "$mode" = install ] && [ "$detached" -eq 1 ]; then
	if [ "$state_present" -eq 1 ]; then
		case "$state_kind" in
		managed)
			if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
				printf '%s\n' 'git hook: detached install kept existing installer-owned snapshot hooks'
			else
				printf 'git hook: skipped: installer ownership state exists but core.hooksPath changed externally; current=%s\n' "${hooks_path:-<unset>}" >&2
			fi
			exit 0
			;;
		legacy-managed|legacy-pending|pending)
			printf '%s\n' 'git hook: detached install preserved existing hook ownership state'
			exit 0
			;;
		esac
	fi
	if [ "$hooks_path_present" -eq 0 ]; then
		printf '%s\n' 'git hook: detached install did not claim hook ownership'
		exit 0
	fi
fi

if [ "$mode" = retire ]; then
	if [ "$state_present" -eq 0 ]; then
		printf '%s\n' 'git hook: retire unchanged: core.hooksPath is not installer-owned'
		exit 0
	fi
	owned_path=$managed_hooks_path
	case "$state_kind" in
	legacy-managed|legacy-pending) owned_path=$legacy_hooks_path ;;
	esac
	if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$owned_path" ]; then
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
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
			install_managed_hooks
			printf '%s\n' 'git hook: refreshed installer-owned snapshot hooks'
		else
			printf 'git hook: skipped: installer ownership state exists but core.hooksPath changed externally; current=%s\n' "${hooks_path:-<unset>}" >&2
		fi
		exit 0
		;;
	pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" != "$managed_hooks_path" ]; then
			printf 'git hook: skipped: pending installer ownership conflicts with current core.hooksPath: %s\n' "$hooks_path" >&2
			exit 0
		fi
		install_managed_hooks
		if [ "$hooks_path_present" -eq 0 ]; then
			if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
				printf '%s\n' 'git hook: failed to recover pending managed hooks activation' >&2
				exit 1
			fi
		fi
		write_state "$managed_state"
		printf '%s\n' 'git hook: recovered interrupted installer-owned snapshot hooks activation'
		exit 0
		;;
	legacy-managed|legacy-pending)
		if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" != "$legacy_hooks_path" ]; then
			printf 'git hook: skipped: legacy ownership state exists but core.hooksPath changed externally; current=%s\n' "$hooks_path" >&2
			exit 0
		fi
		install_managed_hooks
		write_state "$pending_state"
		if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
			printf '%s\n' 'git hook: failed to migrate installer-owned snapshot hooks' >&2
			exit 1
		fi
		write_state "$managed_state"
		printf '%s\n' 'git hook: migrated installer-owned .githooks to snapshot hooks'
		exit 0
		;;
	esac
fi

if [ "$hooks_path_present" -eq 1 ]; then
	if [ "$hooks_path" = "$legacy_hooks_path" ]; then
		printf '%s\n' 'git hook: unchanged: preexisting .githooks retained without claiming ownership'
	else
		printf 'git hook: skipped: preexisting core.hooksPath retained: %s; compose managed hooks with that hook owner manually if desired\n' "${hooks_path:-<empty>}" >&2
	fi
	exit 0
fi

install_managed_hooks
write_state "$pending_state"
if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
	printf '%s\n' 'git hook: failed to enable managed snapshot hooks; pending ownership state retained for recovery' >&2
	exit 1
fi
write_state "$managed_state"
printf '%s\n' 'git hook: enabled installer-owned snapshot hooks'
