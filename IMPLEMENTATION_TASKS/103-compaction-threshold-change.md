# Task: compaction threshold変更

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked C: compaction threshold変更

評価taskと要求保持改善の実測後。
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

- Task 012の完了証跡は`IMPLEMENTATION_HISTORY.md`の`2026-08-28 Task 012 compaction threshold evaluation`を正とする。保存済み20 task・69 call中のboundaryは4 call / 4件で、trigger直前context sizeとcompaction要約costはunknownのため、現時点の採否はNo-Go。明示許可後も同形式再測定と、必要なら別契約の観測追加を先に確定する。

## Purpose

品質を落とさずtokenを削減する。

## Contract

Task 012のevidenceと明示許可に基づく。

## Must not

先行してthresholdを変更しない。

## Acceptance criteria

許可原文をAmendmentsへ保存し、Task 012 artifactを読んでconcrete Contract / Must not / Acceptance criteriaを確定してからACTIVE候補にする。

## Historical invariants

compactionとsession agingを分離。

## Dependencies

none

## Review findings

none

## Current boundary

Task 012 evidenceはNo-Go。permissionと、activation時のconcrete Contract / Must not / Acceptance criteria確定待ち。
