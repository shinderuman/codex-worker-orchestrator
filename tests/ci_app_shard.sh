#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
	echo 'usage: ci_app_shard.sh <normal|race> <shard>' >&2
	exit 2
fi
mode=$1
shard=$2
case "$mode" in
normal) max_shard=5 ;;
race) max_shard=11 ;;
*)
	echo "invalid app test mode: $mode" >&2
	exit 2
	;;
esac
case "$shard" in
0 | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 | 11) ;;
*)
	echo "invalid app test shard: $shard" >&2
	exit 2
	;;
esac
if [ "$shard" -gt "$max_shard" ]; then
	echo "invalid app test shard $shard for $mode" >&2
	exit 2
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/app-ci-shard.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

list_tests() {
	if [ "$mode" = race ]; then
		go test -race -list . ./internal/app > "$tmp/listed"
	else
		go test -list . ./internal/app > "$tmp/listed"
	fi
	grep -E '^(Test|Example|Fuzz)[[:alnum:]_]*$' "$tmp/listed" | LC_ALL=C sort > "$tmp/tests"
	total=$(wc -l < "$tmp/tests")
	if [ "$total" -lt 12 ] || [ -n "$(uniq -d "$tmp/tests")" ]; then
		echo 'app test inventory is missing or has duplicate names' >&2
		exit 1
	fi
	for required in \
		TestTypedParentCompletionWorkflowThroughProductionBoundary \
		TestParentOnlyTaskCompletionThroughProductionBoundary \
		TestParentOnlyReentryThroughProductionBoundary; do
		if ! grep -Fxq "$required" "$tmp/tests"; then
			echo "required app completion test missing: $required" >&2
			exit 1
		fi
	done
}

partition_tests() {
	awk -v mode="$mode" -v shard="$shard" '
    {
      if ($0 == "TestTypedParentCompletionWorkflowThroughProductionBoundary") owner = 0
      else if ($0 == "TestParentOnlyTaskCompletionThroughProductionBoundary") owner = 1
      else if ($0 == "TestParentOnlyReentryThroughProductionBoundary") owner = 2
      else if (mode == "race") { owner = 3 + (other % 9); other++ }
      else { slot = other % 9; owner = (slot < 3 ? slot + 3 : slot - 3); other++ }
      if (owner == shard) print
    }
	' "$tmp/tests" > "$tmp/selected"
	selected=$(wc -l < "$tmp/selected")
	if [ "$selected" -eq 0 ]; then
		echo "empty app test shard: $mode $shard" >&2
		exit 1
	fi
	names=$(paste -sd '|' "$tmp/selected")
	printf 'App tests: %s; mode %s; shard %s; selected %s\n' "$total" "$mode" "$shard" "$selected"
	cat "$tmp/selected"
}

run_tests() {
	if [ "$mode" = race ]; then
		/usr/bin/time -p go test -race -count=1 -run "^($names)$" ./internal/app
	else
		/usr/bin/time -p go test -count=1 -run "^($names)$" ./internal/app
	fi
}

list_tests
partition_tests
run_tests
