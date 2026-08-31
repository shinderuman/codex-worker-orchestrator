# ACTIVE task要求とrun-controlの分離

ACTIVE taskを開始・再開するとき、user messageをdurable task contractへ反映する境界にだけ適用する。

## Durable requirement

- `## Amendments`へ永続化するのは、task固有の要求・制約・Acceptance criteria・既存Sol判断の意味を変更するsemantic deltaだけとする。
- semantic deltaは省略・要約で意味を落とさず、次のmodel call前にtask fileへ反映する。decision/fix本文だけでdurable requirementを差し替えない。
- 既にACTIVE task fileへ置いたsubstantive requirementを`USER_REQUEST`へ重複転記しない。`USER_REQUEST`はtask要旨・task file参照と、その呼出に必要なrun-controlだけに留める。

## Run control

- start/resume許可、現在task後の停止境界、runtime preflight、待機・進捗報告、親Codexのreview/commit/Plan・History更新、最終報告形式は親orchestrationの実行状態であり、userがdurable task contractへ含めると明示した場合を除き`## Amendments`へコピーしない。
- ACTIVE taskの新規開始・再開前に既存glm-worker lifecycle stateから合法な次操作を判断する場合は、`glm-worker --handoff`の`consistent`・`required_action`・`allowed_actions`を正規入口とする。`consistent:false`では開始・再開可否を推測しない。repository lockやtask livenessの詳細診断が必要な場合だけ`--status`を追加で使う。
- run-controlだけのmessageではtask fileを変更しない。
- 1つのmessageにsemantic amendmentとrun-controlが混在する場合、semantic amendmentだけをlosslessに分離してtask fileへ反映し、message全体をamendmentとして転記しない。
- user messageの意味を判定するためのgeneric classifier・追加model callは作らない。親Codexが通常の要求解釈として境界を判断する。

## Execution milestones

- 1つのsemantic Plan taskとして保つべきACTIVE taskでも、実装・検証責務が明確な複数の大きいexecution unitへ分かれると親Codexが判断した場合だけ、最初のGLM delegation前にtask fileへ`## Execution milestones`を追加する。token量・file数等の自動threshold/classifierでは作らない。小さいtaskではこの節を置かない。
- milestoneはsemantic amendmentではなくdurable run-control authorityであり、task-wideのOriginal instruction・Amendments・Contract・Must not・Acceptance criteriaを置換・縮小しない。
- 節本文は次のJSONだけを使う。milestoneは2〜8件、配列順を実行順とし、`id`・`scope`・`acceptance`を必須にする。次のmilestoneを新しいworker contextで開始したい場合、その**次のmilestone側**へ`"fresh_worker": true`を付ける。

```json
{
  "milestones": [
    {"id":"bounded-id","scope":"bounded implementation/validation responsibility","acceptance":"milestone-local completion evidence"},
    {"id":"next-id","scope":"next bounded responsibility","acceptance":"next milestone-local evidence","fresh_worker":true}
  ]
}
```

- wrapperは各milestoneの通常worker lifecycleをそのまま使い、packet validation・rule convergence・guard・必要なparent validationを通過した`IMPLEMENTED`だけをmilestone完了としてruntime stateへ保存する。完了証跡にはcurrent Git snapshotとworker resultを含める。
- 中間milestone完了だけを理由にindependent reviewer/Sol ceremonyへ戻さない。wrapperは同一task内の次milestoneへ進み、最終milestone後だけ既存task-wide review lifecycleへ戻す。`NEEDS_SOL_DECISION`・guard stop・rate/provider stop等は従来どおり途中で親境界へ戻る。
- `fresh_worker:true`は明示されたmilestoneの開始時だけworker sessionを切り替える。全milestoneを機械的にsession rotationしない。fresh workerはcurrent Gitとdurable milestone stateからcompleted scopeを引き継ぎ、完了milestoneを最初から再実装しない。
- 実装が想定より大きいと判明した場合、healthy model callを中断せず、次に自然に親へ戻った境界でcurrent/future milestoneだけを追加・再分割してよい。既に完了したmilestoneの`id`・`scope`・`acceptance`・`fresh_worker`は変更しない。
- userのsemantic amendmentが同時にある場合は従来どおりAmendments等へsemantic deltaを反映し、milestone変更へ混ぜてtask-wide requirement変更を隠さない。

## 境界例

- `ACTIVE taskを開始し、完了したら次taskへ進まず結果を報告する`はrun-controlだけなのでtask fileを変更しない。
- `ACTIVE taskを開始する。production test selectionは導入しない、をMust notへ追加する`は後半だけをdurable requirementとしてtask fileへ反映し、開始・停止・報告手順は転記しない。
