#!/bin/sh
set -eu
repo=$1
home=$2
tmp=$3
go_mod_cache=$4
fixture_task_path=$5
export_lock_pid=
export_writer_pid=
stop_export_processes() {
	if [ -n "$export_writer_pid" ]; then
		kill "$export_writer_pid" 2>/dev/null || true
		wait "$export_writer_pid" 2>/dev/null || true
		export_writer_pid=
	fi
	if [ -n "$export_lock_pid" ]; then
		kill "$export_lock_pid" 2>/dev/null || true
		wait "$export_lock_pid" 2>/dev/null || true
		export_lock_pid=
	fi
}
run_single_export() {
	(
		cd "$repo" || exit 1
		HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" export-bundle
	)
}

(
	cd "$repo" || exit 1
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --authority controller-activate
) >"$tmp/controller-activation.json"
export_attempt_id=$(sed -n 's/.*"attempt_id":"\([^"]*\)".*/\1/p' "$tmp/controller-activation.json")
export_task_path=$(sed -n 's/.*"task_path":"\([^"]*\)".*/\1/p' "$tmp/controller-activation.json")
export_contract_digest=$(sed -n 's/.*"contract_digest":"\([^"]*\)".*/\1/p' "$tmp/controller-activation.json")
test -n "$export_attempt_id" && test -n "$export_task_path" && test -n "$export_contract_digest"
test "$export_task_path" = "$fixture_task_path"
fixture_contract_digest=$(git -C "$repo" show HEAD:"$fixture_task_path" | shasum -a 256 | cut -d' ' -f1)
test "$export_contract_digest" = "$fixture_contract_digest"
repo_physical_export=$(CDPATH='' cd -- "$repo" && pwd -P)
export_lineage=$(printf 'git-common-lineage-v1\0%s\0' "$repo_physical_export/.git" | shasum -a 256 | cut -d' ' -f1)
export_controller_store="$home/.glm-worker/controllers/$export_lineage"
export_runtime_hash=$(printf '%s\0%s\0' "$export_lineage" "$export_task_path" | shasum -a 256 | cut -d' ' -f1)
export_runtime_dir="$home/.glm-worker/sessions/$export_runtime_hash"
export_workflow_lock="$export_controller_store.workflow.lock"

if run_single_export >"$tmp/export-stopped.out" 2>"$tmp/export-stopped.stderr"; then
	printf '%s\n' 'no-arg export succeeded before the runtime task identity existed' >&2
	exit 1
fi
test ! -e "$repo/.glm-worker-parent-actions"
if find "$home/.glm-worker/exports" -type f -name '*.zip' 2>/dev/null | grep -q .; then
	printf '%s\n' 'failed identity export wrote an archive' >&2
	exit 1
fi

mkdir -p "$export_runtime_dir/telemetry" "$export_runtime_dir/events" "$export_runtime_dir/artifacts/runtime-task-smoke-1"
printf '%s' 'runtime-task-smoke-1' >"$export_runtime_dir/task.id"
printf '{"version":1,"attempt_id":"%s","task_path":"%s","task_contract_digest":"%s"}' \
	"$export_attempt_id" "$export_task_path" "$export_contract_digest" >"$export_runtime_dir/controller-runtime-binding.json"
printf '%s' 'session-smoke-worker' >"$export_runtime_dir/worker.id"
printf '%s' '{"version":3,"call_id":"c1","partial' >"$export_runtime_dir/telemetry/runtime-task-smoke-1.jsonl"
printf 'smoke artifact\n' >"$export_runtime_dir/artifacts/runtime-task-smoke-1/report.txt"
printf 'untracked export bytes\n' >"$repo/untracked-export.txt"
git -C "$repo" status --porcelain >"$tmp/export-git-before"
export_generation_before=$(sed -n 's/^  "controller_generation": \([0-9]*\).*/\1/p' "$export_controller_store/head.json")
test -n "$export_generation_before"
find "$home/.glm-worker/sessions" -mindepth 1 -maxdepth 1 | LC_ALL=C sort >"$tmp/export-sessions-before"

run_single_export >"$tmp/export-live.json"
grep -q '"runtime_section":"collected"' "$tmp/export-live.json"
grep -q '"attempt_section":"absent"' "$tmp/export-live.json"
grep -q '"coverage":"partial"' "$tmp/export-live.json"
export_archive_path=$(sed -n 's/.*"archive_path":"\([^"]*\)".*/\1/p' "$tmp/export-live.json")
test -f "$export_archive_path"
unzip -p "$export_archive_path" manifest.json >"$tmp/export-manifest.json"
grep -q 'glm-controller-evidence-export-v2' "$tmp/export-manifest.json"
grep -q '"trailing_fragment": true' "$tmp/export-manifest.json"
grep -q 'live/transcripts/claude/session-smoke-worker' "$tmp/export-manifest.json"
unzip -p "$export_archive_path" live/task/telemetry.jsonl | grep -q 'partial'
unzip -p "$export_archive_path" live/git/untracked/untracked-export.txt | grep -q 'untracked export bytes'
unzip -p "$export_archive_path" analysis-index.json | grep -q '"schema_version": 1'
if unzip -l "$export_archive_path" | grep -q 'object-archive'; then
	printf '%s\n' 'live export embedded a git object archive payload' >&2
	exit 1
fi
(
	cd "$repo" || exit 1
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" export-bundle --task-id runtime-task-smoke-1
) >"$tmp/export-task-id.json"
grep -q '"runtime_section":"collected"' "$tmp/export-task-id.json"
grep -q '"runtime_task_id":"runtime-task-smoke-1"' "$tmp/export-task-id.json"
export_task_id_archive=$(sed -n 's/.*"archive_path":"\([^"]*\)".*/\1/p' "$tmp/export-task-id.json")
test -f "$export_task_id_archive"
test "$export_task_id_archive" = "$export_archive_path"
unzip -p "$export_task_id_archive" manifest.json >"$tmp/export-task-id-manifest.json"
grep -q 'glm-controller-evidence-export-v2' "$tmp/export-task-id-manifest.json"
grep -q '"kind": "task-id"' "$tmp/export-task-id-manifest.json"
grep -q '"runtime_task_id": "runtime-task-smoke-1"' "$tmp/export-task-id-manifest.json"
(
	cd "$repo" || exit 1
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" export-bundle --task-id runtime-task-unknown
) >"$tmp/export-unknown.out" 2>"$tmp/export-unknown.stderr" && exit 1
grep -q 'unknown to the controller' "$tmp/export-unknown.stderr"
git -C "$repo" status --porcelain >"$tmp/export-git-after"
cmp "$tmp/export-git-before" "$tmp/export-git-after"
export_generation_after=$(sed -n 's/^  "controller_generation": \([0-9]*\).*/\1/p' "$export_controller_store/head.json")
test "$export_generation_before" = "$export_generation_after"
find "$home/.glm-worker/sessions" -mindepth 1 -maxdepth 1 | LC_ALL=C sort >"$tmp/export-sessions-after"
cmp "$tmp/export-sessions-before" "$tmp/export-sessions-after"
if (
	cd "$repo" || exit 1
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" controller-evidence definitely-not-a-token
) >"$tmp/export-bad-token.stdout" 2>"$tmp/export-bad-token.stderr"; then
	printf '%s\n' 'glm-parent-action controller-evidence accepted an invalid token' >&2
	exit 1
fi

cat >"$tmp/hold-workflow-lock.go" <<'EOF_HOLD_LOCK'
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	try := len(os.Args) == 3 && os.Args[2] == "--try"
	if len(os.Args) != 2 && !try {
		fmt.Fprintln(os.Stderr, "usage: hold-workflow-lock <path> [--try]")
		os.Exit(2)
	}
	file, err := os.OpenFile(os.Args[1], os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()
	mode := syscall.LOCK_EX
	if try {
		mode |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		if try && (err == syscall.EWOULDBLOCK || err == syscall.EAGAIN) {
			fmt.Fprintln(os.Stderr, "workflow lock already held")
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if try {
		return
	}
	fmt.Println("locked")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
}
EOF_HOLD_LOCK
HOME="$home" GOMODCACHE="$go_mod_cache" go build -o "$tmp/hold-workflow-lock" "$tmp/hold-workflow-lock.go"

if "$tmp/hold-workflow-lock" "$tmp/missing-lock-directory/workflow.lock" --try >"$tmp/export-lock-fault.out" 2>"$tmp/export-lock-fault.stderr"; then
	printf '%s\n' 'lock probe accepted an inaccessible lock path' >&2
	exit 1
else
	lock_fault_status=$?
fi
test "$lock_fault_status" -eq 1
grep -q 'no such file or directory' "$tmp/export-lock-fault.stderr"

wait_for_export_lock() {
	lock_output=$1
	for _ in $(seq 1 100); do
		if grep -Fxq locked "$lock_output" 2>/dev/null; then
			if "$tmp/hold-workflow-lock" "$export_workflow_lock" --try >"$tmp/export-lock-probe.out" 2>"$tmp/export-lock-probe.stderr"; then
				printf '%s\n' 'lock helper reported readiness without holding the workflow lock' >&2
				return 1
			else
				lock_probe_status=$?
				if [ "$lock_probe_status" -ne 3 ]; then
					printf 'lock contention probe failed (exit %s):\n' "$lock_probe_status" >&2
					cat "$tmp/export-lock-probe.stderr" >&2
					return 1
				fi
			fi
			return 0
		fi
		if ! kill -0 "$export_lock_pid" 2>/dev/null; then
			break
		fi
		sleep 0.1
	done
	printf '%s\n' 'lock helper did not become ready' >&2
	return 1
}

if (
	trap 'stop_export_processes' EXIT
	trap 'exit 1' HUP INT TERM
	"$tmp/hold-workflow-lock" "$export_workflow_lock" >"$tmp/export-lock-failure.out" 2>"$tmp/export-lock-failure.stderr" &
	export_lock_pid=$!
	printf '%s\n' "$export_lock_pid" >"$tmp/export-lock-failure.pid"
	wait_for_export_lock "$tmp/export-lock-failure.out" || exit 2
	exit 19
); then
	printf '%s\n' 'expected lock-holder failure-path exit' >&2
	exit 1
else
	export_failure_status=$?
fi
test "$export_failure_status" -eq 19
export_failure_pid=$(cat "$tmp/export-lock-failure.pid")
if kill -0 "$export_failure_pid" 2>/dev/null; then
	printf '%s\n' 'lock helper survived failure-path cleanup' >&2
	exit 1
fi
"$tmp/hold-workflow-lock" "$export_workflow_lock" --try >"$tmp/export-lock-failure-reacquired.out"

"$tmp/hold-workflow-lock" "$export_workflow_lock" >"$tmp/export-lock-held.out" 2>"$tmp/export-lock-held.stderr" &
export_lock_pid=$!
wait_for_export_lock "$tmp/export-lock-held.out"
(
	while :; do
		printf '%s\n' '{"version":3,"call_id":"active-writer"}' >>"$export_runtime_dir/telemetry/runtime-task-smoke-1.jsonl"
		sleep 0.01
	done
) &
export_writer_pid=$!
run_single_export >"$tmp/export-concurrent.json"
export_lock_wait_pid=$export_lock_pid
stop_export_processes
if kill -0 "$export_lock_wait_pid" 2>/dev/null; then
	printf '%s\n' 'lock helper survived successful export cleanup' >&2
	exit 1
fi
"$tmp/hold-workflow-lock" "$export_workflow_lock" --try >"$tmp/export-lock-reacquired.out"
grep -q '"runtime_section":"collected"' "$tmp/export-concurrent.json"
grep -q '"archive_path":"' "$tmp/export-concurrent.json"
export_concurrent_archive=$(sed -n 's/.*"archive_path":"\([^"]*\)".*/\1/p' "$tmp/export-concurrent.json")
test -f "$export_concurrent_archive"
test -f "$export_archive_path"
test "$export_concurrent_archive" = "$export_archive_path"
export_zip_dir=$(dirname "$export_archive_path")
export_zip_count=$(find "$export_zip_dir" -type f -name '*.zip' | wc -l | tr -d ' ')
test "$export_zip_count" = 1
rm -f "$repo/untracked-export.txt"
git -C "$repo" status --porcelain >"$tmp/export-git-restored"
if [ -s "$tmp/export-git-restored" ]; then
	printf '%s\n' 'export scenario left the fixture repository dirty' >&2
	exit 1
fi
