# Task: Docker CLI deny の path と複合command coverage

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#pullrequestreview-5743177560

**Align the Docker Bash deny rules with the path forms used for `go test`.**

`workerGoTestDisallowedBashRules` covers bare, `* go`, and `*/go` forms. `workerDockerDisallowedBashRules` covers only bare `docker <subcommand>` and five subcommands. These commands match neither the Bash rules nor the PATH shim:

- `/usr/local/bin/docker run ...`
- `cd x && docker run ...`

These commands are not in the subcommand list:

- `docker version`
- `docker pull`
- `docker image ...`
- `docker volume ...`

The sandbox Unix socket denial still blocks daemon access, so this is defense in depth. However, `docs/linux-go-tests.md` and `codex/instructions/worker/go.md` state that the Bash deny rule rejects direct `docker` CLI execution. That statement is broader than these rules.

<details>
<summary>♻️ Proposed rule set</summary>

```diff
 var workerDockerDisallowedBashRules = []string{
-	"Bash(docker run*)",
-	"Bash(docker exec*)",
-	"Bash(docker cp*)",
-	"Bash(docker build*)",
-	"Bash(docker compose*)",
+	"Bash(docker *)",
+	"Bash(* docker *)",
+	"Bash(*/docker *)",
 }
```
Update the explicit rule list in `command_policy_test.go` Line 69 to match.
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#pullrequestreview-5743177560) — `glm-worker/internal/runner/runner.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

Docker直接CLIを拒否すると宣言する一方、Bash rulesは一部のbare subcommandだけで絶対path・複合commandを拾わない。

## External feasibility

status: not-applicable

## Contract

- 既存deny ownerをgo test側と整合するbare/absolute/compound command形と全Docker subcommandへ揃える。sandbox Unix socket拒否とPATH shimを維持する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- bare・絶対path・cd/&&等の複合commandとversion/pull/image/volumeが既存policyで拒否される。許可済みowner executorのDocker実行は維持し、新しいrunner/権限/allowlist迂回を作らない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
