# Task: GLM-5-Turbo等model routing再設計

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked B: GLM-5-Turbo等model routing再設計

実測品質証拠と許可条件が揃うまで保留。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
## 11. blocked taskはplaceholder contractのままACTIVE化しない

blocked taskには、

> 許可後の個別contract

だけが書かれているものがあります。

現在blockedである間はそれで構いません。

ただしユーザー許可が出た時に、そのままACTIVEへ昇格して実装開始しないでください。

まず、

1. ユーザー許可をAmendmentへlossless保存
2. prerequisite evaluation artifactを読む
3. concrete Contract
4. Must not
5. Acceptance criteria

をtask fileへ確定する。

その後でACTIVE候補にしてください。

「permission received」だけで設計未確定taskをGLMへ投げないでください。
````

## Resolved references

- Task 013の完了証跡は`IMPLEMENTATION_HISTORY.md`の`2026-08-29 Task 013 worker model routing evaluation`を正とする。current codex-config telemetryはsingle resolved model `glm-5.3`だけで、alias差はmodel品質証拠にならず、routing変更はNo-Go。

## Purpose

品質を維持してprovider costを最適化する。

## Contract

Task 013のevidenceに基づく。

## Must not

sample不足downgradeをしない。

## Acceptance criteria

許可原文をAmendmentsへ保存し、Task 013 artifactを読んでconcrete Contract / Must not / Acceptance criteriaを確定してからACTIVE候補にする。

## Historical invariants

GLM-4.7 sample不足。

## Dependencies

none

## Review findings

none

## Current boundary

Task 013 evidenceはquality delta unknownでrouting変更を支持しない。複数resolved modelを同一repository・role・normalized phase・effective risk・convergence delta groupで比較できる実運用証拠とユーザー許可待ち。
