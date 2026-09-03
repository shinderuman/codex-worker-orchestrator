# Task: unscheduled task stateのGo/No-Go照合

## Original instruction

````text
本当にAcceptanceが未完了なのか？
コミット履歴には存在していたりしないか？
````

## Amendments

````text
履歴が見つからなかった場合の話だがAcceptanceに限らず、どういう状態のファイルなのかわからないため全体的にそのまま作業するべきではないと思う
前段としてそのタスクのGo/No-Goを決めてから作業するべきだと思う
````

## Resolved references

- 対象はPlan外に残っていた4件: `codex-desktop-prompt-overhead-reduction.md`、`codex-instruction-conflict-reduction.md`、`glm-containment-denial-explanations.md`、`parent-finalization-choreography-reduction.md`
- production implementation commitは順に`04b1011`、`a96c0df`、`4a27a70`、`bdf0e4c`として存在する
- `612a8c7`時点のHistoryはcommentlint runが2026-08-28開始の長期親threadであり、fresh-thread context削減と後続Eval Acceptanceは未評価と明記する
- 次commit `171c0ff`は外部ログ監査を理由に4件をPlanから外したが、4 task file自体を更新せず、全件のAcceptance判定とsource locatorをcommitへ残していない
- 後続commitにはfinalize-check cwd修正`240d79b`、parent-owned validation retry抑止`d68dc75`、parent action terminal result統合`7d13e1e`等があり、元taskを部分的または実質的に満たした可能性を個別照合する必要がある

## Purpose

由来・実装・評価状態が混在した4 taskをそのまま再実行せず、現物証拠から各taskの実状態と残作業採否を確定する。

## External feasibility

status: not-applicable

## Contract

- 親Codexが各taskのOriginal instruction、Amendments、Contract、Must not、Acceptance criteria、Current boundaryを独立に照合する
- task file自身の記述だけでなく、全commit履歴、削除済みHistory、関連bundle/telemetry/rollout、後続production commitとcurrent behaviorをsource locator付きで確認する
- 実装済み部分、指定dogfoodで成立/失敗/未観測のAcceptance、後続commitで代替された部分、現在も意味のある残作業を分離する
- 各taskを`GO`（具体的で現在も必要な残作業あり）、`COMPLETE`（current evidenceで全Acceptance成立）、`NO-GO`（不要、重複、成立不能、または費用対効果不成立）のいずれかへSol判断する
- `GO`だけをconcrete Contract / Must not / Acceptance / evidence boundaryへ更新して実行可能にする。`COMPLETE` / `NO-GO`はproduction再実装せずtask fileとPlanを既存lifecycleで同期する
- 判断はCodexが行い、GLMへ調査・採否を委譲する追加model callを行わない

## Must not

- `Current boundary`の「未評価」だけで未完了を断定しない
- implementation commitの存在だけでruntime/effect Acceptanceまで完了扱いしない
- 外部Issue/Web GPT trackerの未収録状態をrepository authorityとして推測しない
- unknownな状態のtaskをworkerへdispatchしない
- 4件のproduction codeをこの照合task内で変更しない

## Acceptance criteria

- 4件すべてについてimplementation、runtime/effect evidence、後続代替、残作業の有無をsource locator付きで確定する
- 各件にGO/COMPLETE/NO-GOと理由があり、unknownのまま実行可能scheduleへ残らない
- GOの場合は再実装を避けた最小のconcrete残作業へtask contractを更新する
- COMPLETE/NO-GOの場合は通常completion/decision boundaryに従いPlan/Task metadataを同期する
- production diff、GLM model call、追加の実Sol A/Bを発生させない
- task corpusとPlan scheduleのclosureを維持する

## Historical invariants

- Git / current tree / Rules / Plan / Taskのauthority順位を維持する
- 通常completion evidenceはGit / CI / bundle / telemetryから回収し、History ledgerを復活させない
- Codex ReductionとQuality Deltaを最上位評価とし、GLM token単独の節約を採用根拠にしない

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。4件のGo/No-Go確定までproduction実装を開始しない。
