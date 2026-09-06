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

- 2026-09-06 permanent operation authorization:

````text
じゃあ恒久的にセッションを新しく作るのを許可するのでそういう運用にしろ
````

- 2026-09-06 adopted operating rule:

  - 通常は2 task完了ごとに親Codex sessionをrotationする
  - HIGH risk task、compaction発生、親5h枠10%以上消費、親model return 5回超、複数回のreview修正・競合復旧・Sol判断、過大model-visible outputのいずれかでは1 taskでrotationする
  - rotationごとのuser再承認を求めない
  - GLM in-flight処理を再起動せず、同じsaved projectのlocal checkoutとcanonical ACTIVE task/runtime stateを新しいCodex taskへ引き継ぐ

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

長寿命sessionで親context再入力が累積する前に、task境界でCodex sessionを機械的にrotationし、品質判断を維持したまま親Codex実消費を抑える。

## External feasibility

status: not-applicable

## Contract

- defaultは2 task完了ごととし、HIGH risk task、compaction、親5h枠10%以上消費、親model return 5回超、複数回のreview修正・競合復旧・Sol判断、過大model-visible outputのいずれかでは1 task完了時または安全なparent handoff境界でrotationする
- ユーザーの恒久許可をauthorityとしてrotationごとの再承認を求めず、新しいCodex taskを同じsaved projectのlocal checkoutに作成する
- `parent-codex-token-attribution.md`と`telemetry-history-cohort-query.md`のfresh evidenceを使い、閾値を調整する。ただしrotation採用自体をNo-Goへ戻さない
- session継続とrotation後bootstrap / authority再読の双方をCodex total tokenで比較し、GLM token削減だけを採用根拠にしない
- 新sessionはPlan ACTIVE task、Git現物、runtime task/session IDだけから再開し、旧会話の自由文を要求正本として複製しない
- 既存state、checkpoint、resume、parent actionを再利用し、rotation専用daemon / DBを追加しない

## Must not

- task境界を無視した無条件rotation、tokenだけのhard cap、compaction閾値変更を導入しない
- healthyなGLM in-flight処理を新規task/sessionとして再起動しない
- rotation先を別worktreeにして現在checkoutに紐づくruntime stateを失わない
- GLM token削減のために親Codex token、Sol判断、要求再説明、authority再読を増やす設計を採用しない
- 品質proxyが悪化する条件、counter resetやtask attributionがunknownな条件を改善扱いしない

## Acceptance criteria

- default 2 taskと早期1 task triggerがtask completion/handoff lifecycleから新しいCodex taskを一度だけ作成し、重複を防ぐ
- rotation先が同じsaved projectのlocal checkout、同じPlan ACTIVE、同じGLM task/session stateを参照し、in-flight処理を再起動しない
- fresh telemetryで継続/rotationのCodex total tokenと品質proxyを比較し、source locator付きで閾値を調整できる
- 既存lifecycleに統合したbounded rule、rollback、tests、独立reviewを完了する
- いずれの結論でもGLM token単独の改善を成功条件にしない

## Historical invariants

session aging telemetry。Task 009 worker outlier report完了済み。Task 010では既存の責務ベース分割と現行resume境界を維持し、hard cap・強制事前分割・強制semantic milestoneは導入しないと決定済み。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/parent-codex-token-attribution.md`
- `IMPLEMENTATION_TASKS/telemetry-history-cohort-query.md`
- `IMPLEMENTATION_TASKS/parent-codex-rollout-chain-attribution.md`

## Review findings

none

## Current boundary

恒久運用をユーザー許可済み。依存taskはすべて完了し、採用済みrotationの機械化を開始できる。fresh evidenceは採否ではなく閾値調整に用いる。
