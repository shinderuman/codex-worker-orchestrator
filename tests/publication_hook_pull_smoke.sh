#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/publication-hook-pull-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

remote="$tmp/remote.git"
repo="$tmp/repo"
publisher="$tmp/publisher"
guard="$tmp/glm-publication-guard"
home="$tmp/home"
mkdir -p "$home"

(
	cd "$source_root/glm-worker"
	go build -buildvcs=false -trimpath -o "$guard" ./cmd/glm-publication-guard
)

git init -q --bare "$remote"
git init -q -b main "$repo"
git -C "$repo" config user.name publication-pull-smoke
git -C "$repo" config user.email publication-pull-smoke@example.invalid
mkdir -p "$repo/.githooks"
cp "$source_root/.githooks/reference-transaction" "$repo/.githooks/reference-transaction"
cp "$source_root/.githooks/pre-push" "$repo/.githooks/pre-push"
cat >"$repo/.githooks/post-merge" <<'EOF_HOOK'
#!/bin/sh
exit 0
EOF_HOOK
cat >"$repo/.githooks/pre-commit" <<'EOF_HOOK'
#!/bin/sh
exit 0
EOF_HOOK
chmod 755 "$repo/.githooks/post-merge" "$repo/.githooks/pre-commit" "$repo/.githooks/reference-transaction" "$repo/.githooks/pre-push"
printf '%s\n' 'github.com/shinderuman/codex-worker-orchestrator/repository-harness/v1' >"$repo/.glm-worker-repository-harness"
printf '%s\n' 'initial' >"$repo/README.md"
git -C "$repo" add .githooks .glm-worker-repository-harness README.md
git -C "$repo" commit -qm initial
git -C "$repo" remote add origin "$remote"
git -C "$repo" push -q -u origin main

install_revision=$(git -C "$repo" rev-parse HEAD)
sh "$source_root/scripts/manage-pull-hook.sh" install "$repo" "$guard"
managed=$(git -C "$repo" config --local --get-all core.hooksPath)
test -x "$managed/reference-transaction"
test -x "$managed/pre-push"
test "$(sed -n '1p' "$managed/glm-publication-guard.path")" = "$guard"
test "$(sed -n '2p' "$managed/glm-publication-guard.path")" = "snapshot=$install_revision"

for hook in reference-transaction pre-push; do
	if grep -Fq 'push-binding' "$source_root/.githooks/$hook" || grep -Fq 'glm-parent-action.path' "$source_root/.githooks/$hook"; then
		printf 'tracked %s still references retired publication surface\n' "$hook" >&2
		exit 1
	fi
	grep -Fq 'glm-publication-guard.path' "$source_root/.githooks/$hook"
done

git clone -q --branch main "$remote" "$publisher"
git -C "$publisher" config user.name publication-publisher
git -C "$publisher" config user.email publication-publisher@example.invalid
printf '%s\n' 'advanced' >>"$publisher/README.md"
git -C "$publisher" add README.md
git -C "$publisher" commit -qm advanced
git -C "$publisher" push -q origin main

before=$(git -C "$repo" rev-parse HEAD)
remote_head=$(git -C "$publisher" rev-parse HEAD)
if [ "$before" = "$remote_head" ]; then
	printf '%s\n' 'publication pull smoke did not establish a behind local branch' >&2
	exit 1
fi

(
	cd "$repo"
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" git pull --ff-only origin main
)

after=$(git -C "$repo" rev-parse HEAD)
test "$after" = "$remote_head"
test "$(sed -n '2p' "$managed/glm-publication-guard.path")" = "snapshot=$install_revision"
