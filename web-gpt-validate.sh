#!/bin/sh
set -eu

if [ "$#" -ne 7 ]; then
	printf '%s\n' 'usage: web-gpt-validate.sh <target-branch> <expected-head-sha> <control-sha> <mode> <scope> <result-json> <artifact-locator>' >&2
	exit 2
fi

target_branch=$1
expected_head_sha=$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]')
control_sha=$(printf '%s' "$3" | tr '[:upper:]' '[:lower:]')
validation_mode=$4
validation_scope=$5
result_path=$6
artifact_locator=$7
target_root=$(pwd -P)
control_root=${WEB_GPT_VALIDATION_CONTROL_ROOT:-}
reported_branch=''
reported_sha=''
reported_control_sha=''
reported_mode=''
reported_scope=''
reported_artifact_locator=''
result=not_run
error_code=''
result_written=false

write_result() {
	printf '{"tested_branch":"%s","tested_sha":"%s","control_sha":"%s","mode":"%s","result":"%s","scope":"%s","artifact_locator":"%s","error":"%s"}\n' \
		"$reported_branch" "$reported_sha" "$reported_control_sha" "$reported_mode" "$result" "$reported_scope" "$reported_artifact_locator" "$error_code" >"$result_path"
	result_written=true
}

on_exit() {
	status=$?
	trap - EXIT
	if [ "$result_written" != true ]; then
		result=fail
		error_code=unexpected_failure
		if ! write_result; then
			:
		fi
		if [ "$status" -eq 0 ]; then
			status=1
		fi
	fi
	exit "$status"
}
trap on_exit EXIT

reject() {
	error_code=$1
	result=rejected
	write_result
	printf 'web-gpt-validation: %s\n' "$error_code" >&2
	exit 1
}

validate_sha() {
	value=$1
	if [ "${#value}" -ne 40 ]; then
		return 1
	fi
	case "$value" in
	*[!0-9a-f]*) return 1 ;;
	esac
}

case "$target_branch" in
web-gpt/*) ;;
*) reject invalid_target_branch ;;
esac
if ! printf '%s\n' "$target_branch" | grep -Eq '^web-gpt/[A-Za-z0-9._/-]+$'; then
	reject invalid_target_branch
fi
if ! git check-ref-format "refs/heads/$target_branch" >/dev/null 2>&1; then
	reject invalid_target_branch
fi
reported_branch=$target_branch
if ! validate_sha "$expected_head_sha"; then
	reject invalid_expected_head_sha
fi
reported_sha=$expected_head_sha
if ! validate_sha "$control_sha"; then
	reject invalid_control_sha
fi
reported_control_sha=$control_sha
if ! printf '%s\n' "$artifact_locator" | grep -Eq '^[A-Za-z0-9._-]+$'; then
	reject invalid_artifact_locator
fi
reported_artifact_locator=$artifact_locator
case "$validation_mode" in
repository-lint | full-go-test | build-vet)
	reported_mode=$validation_mode
	if [ -n "$validation_scope" ]; then
		reject unexpected_scope
	fi
	;;
go-package-test)
	reported_mode=$validation_mode
	if ! printf '%s\n' "$validation_scope" | grep -Eq '^\./[A-Za-z0-9_][A-Za-z0-9_.-]*(/[A-Za-z0-9_][A-Za-z0-9_.-]*)*$'; then
		reject invalid_package_scope
	fi
	reported_scope=$validation_scope
	;;
*) reject invalid_validation_mode ;;
esac
case "$control_root" in
/*) ;;
*) reject invalid_control_root ;;
esac
if [ ! -d "$control_root" ] || [ ! -f "$control_root/quality-tools.yml" ]; then
	reject invalid_control_root
fi
if ! actual_control_sha=$(git -C "$control_root" rev-parse HEAD); then
	reject control_sha_unavailable
fi
if [ "$actual_control_sha" != "$control_sha" ]; then
	reject control_sha_mismatch
fi
if ! local_head=$(git rev-parse HEAD); then
	reject local_head_unavailable
fi
if [ "$local_head" != "$expected_head_sha" ]; then
	reject stale_local_checkout
fi
if ! remote_head=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }'); then
	reject target_branch_missing
fi
if [ "$remote_head" != "$expected_head_sha" ]; then
	reject stale_expected_head
fi
if ! status=$(git status --porcelain); then
	reject git_status_failed
fi
if [ -n "$status" ]; then
	reject dirty_checkout
fi
if git config --local --get-regexp '^(http\..*\.extraheader|credential\.|includeIf\.)' >/dev/null 2>&1; then
	reject persisted_git_credentials
fi

go_version=$(awk -F': ' '$1 == "go" { print $2; exit }' "$control_root/quality-tools.yml")
if ! printf '%s\n' "$go_version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	reject invalid_control_go_version
fi
export GOTOOLCHAIN="go$go_version"
export GOCACHE="${RUNNER_TEMP:-${TMPDIR:-/tmp}}/codex-worker-orchestrator-focused-go-$go_version"

run_repository_lint() {
	(
		cd "$control_root"
		HARNESSLINT_REPO_ROOT="$target_root" HARNESSLINT_CONTROL_ROOT="$control_root" ./harnesslint --controlled-check
	)
}

run_go_package_test() {
	go -C "$target_root/glm-worker" test "$validation_scope"
}

run_full_go_test() {
	go -C "$target_root/glm-worker" test ./...
}

run_build_vet() {
	go -C "$target_root/glm-worker" vet ./...
	go -C "$target_root/glm-worker" build ./...
}

set +e
case "$validation_mode" in
repository-lint)
	run_repository_lint
	validation_status=$?
	;;
go-package-test)
	run_go_package_test
	validation_status=$?
	;;
full-go-test)
	run_full_go_test
	validation_status=$?
	;;
build-vet)
	run_build_vet
	validation_status=$?
	;;
*)
	set -e
	reject invalid_validation_mode
	;;
esac
set -e

if [ "$validation_status" -ne 0 ]; then
	result=fail
	error_code=validation_failed
	write_result
	exit "$validation_status"
fi
result=pass
write_result
