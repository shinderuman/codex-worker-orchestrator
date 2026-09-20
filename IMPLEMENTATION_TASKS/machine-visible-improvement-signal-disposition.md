# Task: machine-visible improvement signal disposition

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- current Rulesは、反復可能な手作業・高コスト処理・不要parent return・重複validation・過大model-visible output・観測欠損等を、ユーザーの個別指摘を待たずparentが改善Findingとして評価・Task化することを要求している
- Dogfoodではterminal truncation、stale handoff誤帰属、required Gate override等の明確な異常をparent自身が先にTask化せず、user指摘後に初めて採用した
- `detected-defect-task-registration-enforcement.md` はparentが既にverified findingとして採用した後のdurable registrationを所有する。本taskはその前段を扱う

## Purpose

既にmachine-readableに観測できる高信号の異常・コスト・失敗イベントについて、parentがユーザー催促なしに明示dispositionし、adoptしたFindingを既存registration lifecycleへ接続する。

## External feasibility

status: not-applicable

## Contract

- machine-readableに観測できる高信号の異常/コスト/失敗イベントについて、parentがユーザー催促なしに明示dispositionするnormal pathを作る
- dispositionは少なくとも `adopt / existing-owner / duplicate / reject / awaiting-evidence` 相当をboundedに記録できる
- generic natural-language defect detectorや追加classifier model callを作らない
- semanticに新規Findingかどうかの最終判断はparent/Solに残す
- adoptされたFindingは `detected-defect-task-registration-enforcement.md` のdurable registration lifecycleへ接続する
- Dogfood/telemetryで既に機械的に得られるsignalを優先し、自由文から任意の問題を全自動抽出しない

## Must not

- すべてのwarningを自動Task化しない
- continuous-improvement全体を新しい巨大state machineへ置き換えない
- semantic parent judgmentをGLMへ移さない
- rejected / duplicate / existing-owner signalを新規Taskへ増殖させない

## Acceptance criteria

- representativeなmachine-visible anomaly/cost signalが出た場合、user promptなしでparent dispositionが要求される
- `adopt`されたfindingは既存durable registrationへ接続される
- duplicate/existing-owner/reject/awaiting-evidenceを明示でき、無条件Task増殖しない
-追加model callなし、Quality Deltaを弱めない
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- semantic Finding採否はparent/Solが所有する
- 採用後のdurable registrationと採用前dispositionを混同しない

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/detected-defect-task-registration-enforcement.md`
