#!/bin/sh
set -eu

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
	printf '%s\n' 'usage: web-gpt-autofix.sh <target-branch> <expected-head-sha> [result-json]' >&2
	exit 2
fi

target_branch=$1
expected_head_sha=$(printf '%s' "$2" | tr '[:upper:]' '[:lower:]')
result_path=${3:-web-gpt-autofix-result.json}
target_root=$(pwd -P)
control_root=${WEB_GPT_AUTOFIX_CONTROL_ROOT:-}
changed=false
resulting_head=$expected_head_sha
validation=not_run
publication=not_started
error_code=''

write_result() {
	printf '{"before_sha":"%s","changed":%s,"resulting_head":"%s","validation":"%s","publication":"%s","error":"%s"}\n' \
		"$expected_head_sha" "$changed" "$resulting_head" "$validation" "$publication" "$error_code" >"$result_path"
}

fail_closed() {
	error_code=$1
	publication=rejected
	write_result
	printf 'web-gpt-autofix: %s\n' "$error_code" >&2
	exit 1
}

run_harnesslint() {
	(
		cd "$control_root"
		HARNESSLINT_REPO_ROOT="$target_root" ./harnesslint "$@"
	)
}

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

case "$control_root" in
/*) ;;
*) fail_closed invalid_control_root ;;
esac
if [ ! -d "$control_root" ] || [ ! -f "$control_root/harnesslint" ]; then
	fail_closed invalid_control_root
fi

if [ -n "$(git status --porcelain)" ]; then
	fail_closed dirty_checkout
fi

remote_head=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }') || fail_closed target_branch_missing
if [ "$remote_head" != "$expected_head_sha" ]; then
	fail_closed stale_expected_head
fi

git fetch --no-tags origin "refs/heads/$target_branch"
fetched_head=$(git rev-parse FETCH_HEAD)
if [ "$fetched_head" != "$expected_head_sha" ]; then
	fail_closed stale_expected_head
fi

git checkout --detach "$expected_head_sha"

set +e
run_harnesslint --fix
fix_status=$?
run_harnesslint
lint_status=$?
set -e

if ! git diff --quiet --; then
	changed=true
fi

if [ "$fix_status" -ne 0 ] || [ "$lint_status" -ne 0 ]; then
	validation=fail
	publication=not_published
	error_code=lint_validation_failed
	write_result
	exit 1
fi
validation=pass

if [ -n "$(git ls-files --others --exclude-standard)" ]; then
	fail_closed unexpected_untracked_autofix_output
fi

if [ "$changed" = false ]; then
	publication=unchanged
	write_result
	exit 0
fi

git config user.name 'github-actions[bot]'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
git add -u
if git diff --cached --quiet --; then
	changed=false
	publication=unchanged
	write_result
	exit 0
fi

git commit -m 'Apply deterministic repository autofixes'
resulting_head=$(git rev-parse HEAD)

remote_before_publish=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }') || fail_closed target_branch_missing
if [ "$remote_before_publish" != "$expected_head_sha" ]; then
	fail_closed concurrent_branch_movement
fi

if ! git push origin "HEAD:refs/heads/$target_branch"; then
	fail_closed publication_rejected
fi

published_head=$(git ls-remote --exit-code origin "refs/heads/$target_branch" | awk 'NR == 1 { print $1 }') || fail_closed publication_verification_failed
if [ "$published_head" != "$resulting_head" ]; then
	fail_closed publication_verification_failed
fi

publication=pushed
write_result
