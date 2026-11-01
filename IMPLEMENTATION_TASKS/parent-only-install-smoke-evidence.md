# Task: parent-only Taskのinstall smoke evidence読み取り境界

## Original instruction

````text
parent-only Taskがrequire_install=trueを宣言した場合のinstall完了経路を、runtime bindingの捏形やrequire_install拒否なしに正規化する。設計はparent-only-install-smoke-evidence-design.md。承認をAmendmentsへ確定してから実装する。
````

## Amendments

### 2026-11-01 親設計判断

attempt.ExecutionAuthorityに基づくreader分岐と既存WorkflowTaskConfigのtask scoped smoke storeを採用する。Runtime bindingは捏造しない。

設計表の「store pathにContractDigestが使われる」は現行codeと不一致であり却下する。WorkflowTaskConfigはLineageIDとTaskPathのみからpathを導く。したがってpath導出だけでAttempt/Contract同一性を証明したと扱わない。parent-onlyのtask.idを当該AttemptIDへ一意に束縛し、readerは非空に加え当該AttemptIDとの一致を検証する。既存materialization ownerの回復可能なeffect段階でidempotentにidentityを成立させ、未確定plan作成時の無管理副作用を追加しない。別attempt・旧contractのsmoke流用を拒否するtestを含める。candidate checkout/source・installed binary・runID・status/exit/completion・repository/log locator・evidenceの既存検証を保持する。

## Resolved references

- GR-1441-2 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1441#discussion_r4434912057 · `glm-worker/internal/controller/publication_parent_install.go:122-127`（`readCandidateInstallSmoke`）
  原文: "P1 **Parent-only installation cannot finish** A parent-only task can declare `require_install=true`, but its successful install can never be recorded here. `boundAttemptRuntime` returns `nil` without a runtime binding, and parent-only execution deliberately has no such binding. After the installer and smoke test succeed, this check returns `candidate install smoke identity is unavailable`. Promotion remains blocked, and retries repeat the installation. Read the task's smoke evidence without requiring a model runtime for parent-only attempts."
- 設計案: `/Users/shinderumanm/.glm-worker/sessions/a25858d98a1498ee2096ef42d60ff4dff9b5dc5e8c3c02301a55d34f1b9859e3/artifacts/716e1f06-9150-4b76-b83d-5638ad5c538c/parent-only-install-smoke-evidence-design.md`。

## Purpose

parent-only installを1回のinstallで完結させ、smoke evidenceをworker経路と同じ厳格さでcandidateへ束縛する。

## External feasibility

status: not-applicable

## Contract

- 承認済み設計に従い、readerをattempt authorityで分岐し、parent-only attemptは`AdmitParentOnlyExecution`時点で記録された非空`task.id`を持つ同一task scoped storeからsmokeを読む。runtime bindingは書かない。
- identity検証（`candidateInstallSmokeMatches`＋log locator）はworker経路と同一関数・同一条件を適用する。
- install evidence record（AttemptID/SnapshotID/CandidateID/CommitOID/TreeOID bound）へのsmoke全文格納は現行維持。

## Must not

- `controller-runtime-binding.json`をparent-onlyに対して生成しない。`require_install`拒否へ縮退させない。第二state・新directory・RunRecord schema拡張をしない。

## Acceptance criteria

- 設計doc第3節の負case 1–5（正常系E2E・別task/runID/fail/未完了/locator不正/Repository不一致拒否・空task.id拒否・分岐回帰・retry時install非重複）が自動testで成立すること。

## Historical invariants

- worker経路のbinding検証を弱めない。candidate evidenceの同一性検証を緩めない。

## Dependencies

none
