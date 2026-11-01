# Task: git実行境界の正確性（gitexec と evidence archive）

## Original instruction

````text
gitexec.Commandの呼出境界とgit local環境変数解決の正確性を修正する。
````

## Amendments

none

## Resolved references

- CR-1440-N2 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#pullrequestreview-5727231289（nitpick: `glm-worker/internal/controller/evidence_git_archive.go:387-393`）
  原文: "**Pass the Git target explicitly instead of rebuilding `command.Args`.** `runGitCommand` builds a `gitexec.Command` for `\".\"`. For `-C` and `--git-dir` it discards that command and builds a second one. For `--git-dir` it then overwrites `command.Args` to remove the `-C` that `gitexec.Command` added. This depends on `gitexec.Command` internals and is fragile. The `init --bare ... <repo>` call in `verifyGitObjectPack` also goes through `runGitBinary(\"\", ...)`. Its target is `\".\"`, so `gitexec` never filters inherited `GIT_*` variables for it, although the new repository is in a different location. This matters if the code runs with `GIT_DIR` or similar variables set, for example from a hook. Give `runGitCommand` an explicit target directory and a flag for `--git-dir`. Alternatively, add a `gitexec.CommandGitDir` helper. Pass the temporary repository path as the target for `init`."
- CR-1440-N3 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#pullrequestreview-5727231289（nitpick: `glm-worker/internal/gitexec/command.go:12-22`）
  原文: "**`localVariables` ignores `gitPath` and caches failures permanently.** `Command` accepts `gitPath`, but `localVariables` always runs `\"git\"` from `PATH`. Callers such as `ResolveGitHeadAuthority(gitPath, ...)` can pass a non-default binary. The Git-local variable list can then come from a different Git, or fail if `\"git\"` is not on `PATH`. `sync.OnceValues` also caches the first error for the life of the process. One transient failure of `git rev-parse --local-env-vars` makes every later cross-worktree `Command` fail. Key the cache by `gitPath` and store successful results only. `environment` must then take `gitPath`. Update the call in `Command` and the test call in `command_test.go`."

## Purpose

git実行のtarget・環境分離・binary同一性が明示的で再試行可能であること。

## External feasibility

status: not-applicable

## Contract

- `runGitCommand`系は明示的target（＋必要なら`--git-dir`）を取り、`command.Args`の事後rewriteをやめる。`verifyGitObjectPack`の`init --bare`は対象repo pathをtargetに渡し`GIT_*` filterを効かせる。
- `localVariables`は`gitPath`を受け取り、成功結果のみを`gitPath`別にcacheする（errorを恒久cacheしない）。

## Must not

- gitexecの公開API意味（env分離の保証）を弱めない。

## Acceptance criteria

- `GIT_DIR`等が設定された環境下でのevidence archive操作の回帰testと、gitPath別cache・error再試行のtest。

## Historical invariants

none

## Dependencies

none
