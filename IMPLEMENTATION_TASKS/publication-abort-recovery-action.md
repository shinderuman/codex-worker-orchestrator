# Task: publication abort 後の修復 action を保持

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607563

** Minor** · **Functional Correctness** · **Quick win**

**Preserve a repair action when candidate reentry admission fails.**

When the abort remote equals the integration tip, a failure from `admitPromotedCandidateReentry` returns an empty `PublicationAdvancementRequired.Action`. A remote race can cause this failure after `candidateReentrySource` observes external advancement. Return `publicationAbortAction(op)` with the reason so callers retain the adopt or rebind action.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>

```diff
 	if err := s.admitPromotedCandidateReentry(head, candidate); err != nil {
-		return "", err.Error()
+		return publicationAbortAction(op), err.Error()
 	}
```

</dd>
</dl>
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607563) — `glm-worker/internal/controller/publication_abort.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

abort時のremote再観測が進行を検出するとcandidate reentryが拒否され、空actionだけが返る。

## External feasibility

status: not-applicable

## Contract

- remote race後にも実行可能なadopt/rebind等のtyped修復actionを現在authorityに照らして返す。無関係な証拠破損まで一律のfallbackで隠さない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- abort remote=integration tipの後に外部進行するfixtureで修復actionと理由が残り、remote/sourceを再検証して復旧できる。候補欠損・証拠無効・異attemptは成功や無条件rebindへ変換しない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
