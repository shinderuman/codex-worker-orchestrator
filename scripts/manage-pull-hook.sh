#!/bin/sh
set -eu

mode=${1:-}
repo_root=${2:-}
managed_hooks_path=.githooks
managed_state='version=1 baseline=absent value=.githooks'

if [ "$mode" != install ] && [ "$mode" != retire ]; then
	printf '%s\n' 'usage: manage-pull-hook.sh <install|retire> <repository>' >&2
	exit 2
fi
if [ -z "$repo_root" ]; then
	printf '%s\n' 'repository path is required' >&2
	exit 2
fi
if ! git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
	printf '%s\n' 'git hook: skipped: not a Git repository'
	exit 0
fi

state_path=$(git -C "$repo_root" rev-parse --git-path codex-worker-orchestrator/hooks-path.state)
case "$state_path" in
/*) ;;
*) state_path="$repo_root/$state_path" ;;
esac

state_present=0
if [ -e "$state_path" ] || [ -L "$state_path" ]; then
	if [ ! -f "$state_path" ] || [ -L "$state_path" ]; then
		printf 'git hook ownership state is not a regular file: %s\n' "$state_path" >&2
		exit 1
	fi
	if [ "$(cat "$state_path")" != "$managed_state" ]; then
		printf 'git hook ownership state is invalid: %s\n' "$state_path" >&2
		exit 1
	fi
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

if [ "$mode" = retire ]; then
	if [ "$state_present" -eq 0 ]; then
		printf '%s\n' 'git hook: retire unchanged: core.hooksPath is not installer-owned'
		exit 0
	fi
	if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
		git -C "$repo_root" config --local --unset-all core.hooksPath
		printf '%s\n' 'git hook: retired installer-owned core.hooksPath'
	else
		printf '%s\n' 'git hook: retire preserved externally changed core.hooksPath'
	fi
	rm -f "$state_path"
	exit 0
fi

chmod +x "$repo_root/.githooks/post-merge"
if [ "$state_present" -eq 1 ]; then
	if [ "$hooks_path_present" -eq 1 ] && [ "$hooks_path" = "$managed_hooks_path" ]; then
		printf '%s\n' 'git hook: unchanged: installer-owned .githooks'
	else
		printf 'git hook: skipped: installer ownership state exists but core.hooksPath changed externally; current=%s\n' "${hooks_path:-<unset>}" >&2
	fi
	exit 0
fi

if [ "$hooks_path_present" -eq 1 ]; then
	if [ "$hooks_path" = "$managed_hooks_path" ]; then
		printf '%s\n' 'git hook: unchanged: preexisting .githooks retained without claiming ownership'
	else
		printf 'git hook: skipped: preexisting core.hooksPath retained: %s; compose .githooks/post-merge with that hook owner manually if desired\n' "${hooks_path:-<empty>}" >&2
	fi
	exit 0
fi

state_dir=${state_path%/*}
mkdir -p "$state_dir"
tmp_state="$state_path.tmp.$$"
trap 'rm -f "$tmp_state"' EXIT HUP INT TERM
umask 077
printf '%s\n' "$managed_state" >"$tmp_state"
mv "$tmp_state" "$state_path"
trap - EXIT HUP INT TERM
if ! git -C "$repo_root" config --local core.hooksPath "$managed_hooks_path"; then
	rm -f "$state_path"
	printf '%s\n' 'git hook: failed to enable .githooks; ownership state rolled back' >&2
	exit 1
fi
printf '%s\n' 'git hook: enabled installer-owned .githooks'
