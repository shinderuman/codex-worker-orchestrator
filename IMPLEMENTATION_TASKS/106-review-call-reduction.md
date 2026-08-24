# Task: review/fix model call縮小

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked F: review/fix model call縮小

convergence実測後。

同一snapshot / verification-only / non-semantic round等が安全に縮小できる証拠が出た場合のみ。
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

- 2026-08-23 convergence baseline:

````text
追加AI callなしの`glm-worker --convergence`全task集計では、codex-configは23 review round中semantic-change 19、doc-change 2、verification-only 1、same-snapshot 1だった。非semantic 4 roundにもreviewer callがあり、合計reviewer durationは約1,983秒だった。media-backupは4 round中semantic 2、same-snapshot 1、verification-only 1だが全体4 sampleに留まる。

削減候補は存在するが、非semantic roundを省略・downgradeしてもqualityを維持できる比較証拠はない。BLOCKEDを維持し、Task 009 / parent review outcome telemetryの結果でCodex差し戻しや後続欠陥が増えない条件を確定してから具体化すること。
````

## Purpose

review品質を維持してmodel callを削減する。

## Contract

convergence/quality evidenceと明示許可に基づく。

## Must not

reviewer省略を先行導入しない。

## Acceptance criteria

許可原文をAmendmentsへ保存し、Task 008 / 009 artifactを読んでconcrete Contract / Must not / Acceptance criteria / rollbackを確定してからACTIVE候補にする。

## Historical invariants

reviewer FIX_REQUIRED率、risk floor。完了済みparent review outcome telemetryにより、Codex新規検出とGLM reviewer既記載の差戻しを分離したorigin別outcome / reworkを追加AI callなしで観測できる。Task 008の固定入力測定ではmachine JSONのbytes/token proxy削減は不立証で、意味保持を採用根拠とした。

## Dependencies

- `IMPLEMENTATION_TASKS/009-worker-call-outliers.md`

## Review findings

none

## Current boundary

2026-08-23 baselineで非semantic review候補は観測したが品質比較証拠とpermissionがないため、evidence/permission待ちを維持。
