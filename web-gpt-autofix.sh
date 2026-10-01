#!/bin/sh
set -eu

if [ "$#" -ne 5 ]; then
	printf '%s\n' 'usage: web-gpt-autofix.sh <prepare|publish> <target-branch> <expected-head-sha> <patch-file> <result-json>' >&2
	exit 2
fi

mode=$1
target_branch=$2
expected_head_sha=$(printf '%s' "$3" | tr '[:upper:]' '[:lower:]')
patch_path=$4
result_path=$5
target_root=$(pwd -P)
control_root=${WEB_GPT_AUTOFIX_CONTROL_ROOT:-}
before_sha=''
changed=false
resulting_head=''
validation=not_run
publication=not_started
error_code=''
result_written=false

write_result() {
	printf '{"before_sha":"%s","changed":%s,"resulting_head":"%s","validation":"%s","publication":"%s","error":"%s"}\n' \
		"$before_sha" "$changed" "$resulting_head" "$validation" "$publication" "$error_code" >"$result_path"
	result_written=true
}

on_exit() {
	status=$?
	trap - EXIT
	if [ "$result_written" != true ]; then
		error_code=unexpected_failure
		publication=rejected
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

fail_closed() {
	error_code=$1
	publication=${2:-rejected}
	write_result
	printf 'web-gpt-autofix: %s\n' "$error_code" >&2
	exit 1
}

validate_target() {
	case "$target_branch" in
	web-gpt/*) ;;
	*) fail_closed invalid_target_branch ;;
	esac
	if ! git check-ref-format "refs/heads/$target_branch" >/dev/null 2>&1; then
		fail_closed invalid_target_branch
	fi
	if [ "${#expected_head_sha}" -ne 40 ]; then
		fail_closed invalid_expected_head_sha
	fi
	case "$expected_head_sha" in
	*[!0-9a-f]*) fail_closed invalid_expected_head_sha ;;
esac
	before_sha=$expected_head_sha
	resulting_head=$expected_head_sha
	if ! status=$(git status --porcelain); then
		fail_closed git_status_failed
	fi
	if [ -n "$status" ]; then
		fail_closed dirty_checkout
	fi
	if ! local_head=$(git rev-parse HEAD); then
		fail_closed local_head_unavailable
	fi
	if [ "$local_head" != "$expected_head_sha" ]; then
		fail_closed stale_local_checkout
	fi
	if ! remote_head=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }'); then
		fail_closed target_branch_missing
	fi
	if [ "$remote_head" != "$expected_head_sha" ]; then
		fail_closed stale_expected_head
	fi
}

run_controlled_harnesslint() {
	(
		cd "$control_root"
		HARNESSLINT_REPO_ROOT="$target_root" HARNESSLINT_CONTROL_ROOT="$control_root" ./harnesslint "$@"
	)
}

capture_patch() {
	output_path=$1
	if ! git diff --binary --full-index --no-ext-diff HEAD -- >"$output_path"; then
		fail_closed patch_generation_failed not_published
	fi
}

require_no_untracked() {
	if ! untracked=$(git ls-files --others --exclude-standard); then
		fail_closed git_inventory_failed
	fi
	if [ -n "$untracked" ]; then
		fail_closed unexpected_untracked_autofix_output
	fi
}

prepare_autofix() {
	case "$control_root" in
	/*) ;;
	*) fail_closed invalid_control_root ;;
	esac
	if [ ! -d "$control_root" ] || [ ! -f "$control_root/harnesslint" ]; then
		fail_closed invalid_control_root
	fi
	validate_target
	set +e
	run_controlled_harnesslint --deterministic-fix
	fix_status=$?
	set -e
	if [ "$fix_status" -ne 0 ]; then
		validation=fail
		fail_closed deterministic_fix_failed not_published
	fi
	require_no_untracked
	capture_patch "$patch_path"
	if [ -s "$patch_path" ]; then
		changed=true
	fi
	set +e
	run_controlled_harnesslint --controlled-check
	check_status=$?
	set -e
	require_no_untracked
	validation_patch=${patch_path}.validation
	capture_patch "$validation_patch"
	if ! cmp -s "$patch_path" "$validation_patch"; then
		rm -f "$validation_patch"
		validation=fail
		fail_closed validation_mutated_target not_published
	fi
	rm -f "$validation_patch"
	if [ "$check_status" -ne 0 ]; then
		validation=fail
		fail_closed lint_validation_failed not_published
	fi
	validation=pass
	if [ "$changed" = false ]; then
		publication=unchanged
		write_result
		return
	fi
	publication=prepared
	write_result
}

publish_autofix() {
	validate_target
	validation=pass
	if [ ! -s "$patch_path" ]; then
		fail_closed missing_prepared_patch
	fi
	if ! git apply --check "$patch_path"; then
		fail_closed patch_check_failed
	fi
	if ! git apply --index "$patch_path"; then
		fail_closed patch_apply_failed
	fi
	if git diff --cached --quiet --; then
		fail_closed empty_prepared_patch
	fi
	if git diff --cached --name-status | grep -Eq '^(A|D|R|C|T)'; then
		fail_closed unexpected_autofix_inventory_change
	fi
	changed=true
	git config user.name 'github-actions[bot]' || fail_closed git_identity_failed
	git config user.email '41898282+github-actions[bot]@users.noreply.github.com' || fail_closed git_identity_failed
	if ! git commit --no-verify -m 'Apply deterministic repository autofixes'; then
		fail_closed commit_failed
	fi
	if ! resulting_head=$(git rev-parse HEAD); then
		fail_closed resulting_head_unavailable
	fi
	if ! remote_before_publish=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }'); then
		fail_closed target_branch_missing
	fi
	if [ "$remote_before_publish" != "$expected_head_sha" ]; then
		fail_closed concurrent_branch_movement
	fi
	if ! git push origin "HEAD:refs/heads/$target_branch"; then
		fail_closed publication_rejected
	fi
	if ! published_head=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }'); then
		fail_closed publication_verification_failed
	fi
	if [ "$published_head" != "$resulting_head" ]; then
		fail_closed publication_verification_failed
	fi
	publication=pushed
	write_result
}

case "$mode" in
prepare) prepare_autofix ;;
publish) publish_autofix ;;
*) fail_closed invalid_mode ;;
esac
