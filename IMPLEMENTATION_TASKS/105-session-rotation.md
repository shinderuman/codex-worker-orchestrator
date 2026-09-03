# Task: session rotation

## Original instruction

````text
# 14. BLOCKED / USER PERMISSION WAITとして保持するtask

以下はtask fileを作ってよいが、`blocked-user-permission`とし自動開始しない。

---

## Blocked E: session rotation

session aging実測後。
compactionとは別論点。
````

## Amendments

- 2026-09-03 user permission and priority:

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

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

## Purpose

長寿命sessionの品質/cost劣化がある場合だけ対策する。

## Contract

- `parent-codex-token-attribution.md`と`telemetry-history-cohort-query.md`のfresh evidenceを使い、session age、compaction、親model/tool turn、model-visible tool bytes、task boundary、Codex review outcomeの関係を比較する
- session継続とrotation後bootstrap / authority再読の双方をCodex total tokenで比較し、GLM token削減だけを採用根拠にしない
- 品質proxyとattributionが十分な場合だけ、既存lifecycleへ収まるbounded rotation ruleを実装する。証拠が不足またはCodex tokenが増える場合はNo-Goとして完了する
- 既存state、checkpoint、resume、parent actionを再利用し、rotation専用daemon / DBを追加しない

## Must not

- 無条件rotation、tokenだけのhard cap、compaction閾値変更を導入しない
- GLM token削減のために親Codex token、Sol判断、要求再説明、authority再読を増やす設計を採用しない
- 品質proxyが悪化する条件、counter resetやtask attributionがunknownな条件を改善扱いしない

## Acceptance criteria

- fresh telemetryで継続/rotationのCodex total tokenと品質proxyを比較し、source locator付きの採否根拠が得られる
- 採用時は既存lifecycleに統合したbounded rule、rollback、tests、独立reviewを完了する
- No-Go時はproduction behaviorを変更せず、再評価に必要な欠損fieldを明示する
- いずれの結論でもGLM token単独の改善を成功条件にしない

## Historical invariants

session aging telemetry。Task 009 worker outlier report完了済み。Task 010では既存の責務ベース分割と現行resume境界を維持し、hard cap・強制事前分割・強制semantic milestoneは導入しないと決定済み。

## Dependencies

- `IMPLEMENTATION_TASKS/parent-codex-token-attribution.md`
- `IMPLEMENTATION_TASKS/telemetry-history-cohort-query.md`

## Review findings

none

## Current boundary

ユーザー許可済み。dependencies完了後にfresh evidenceで採否する。
