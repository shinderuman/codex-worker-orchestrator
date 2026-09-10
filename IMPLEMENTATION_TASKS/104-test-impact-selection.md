# Task: test impactによるtest省略

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked D: test impactによるtest省略

品質証拠後。
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

### 2026-09-10 user permission

````text
なおBLOCKEDも進められるなら進めてよい
````

本taskについてユーザー許可は受領済み。品質証拠が不足した状態ではtest省略を開始しない。

## Purpose

verification cost削減可能性を安全に採否する。

## Contract

Task 014のevidenceと追加品質証拠に基づく。

## Must not

品質証拠なしにtestを削らない。

## Acceptance criteria

追加品質証拠が得られた後、Task 014 artifactを読んでconcrete selection Contract / Must not / Acceptance criteria / rollbackを確定してからACTIVE候補にする。

## Historical invariants

full test gate。

Task 014は完了済み。既存event/telemetry/roundではtest call数・duration・failure outcomeまで測定できるがsuite-level coverageとper-suite failure / escaped contrastはunknownで、omission candidateは提示されなかった。このevidence不足をtest省略の根拠にしない。

## Dependencies

none

## Review findings

none

## Current boundary

ユーザー許可は2026-09-10に受領済み。Task 014のevidenceはtest省略判断に不十分なため、追加品質証拠待ちとしてBLOCKEDを維持する。
