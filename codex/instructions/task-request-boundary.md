# ACTIVE task要求とrun-controlの分離

ACTIVE taskを開始・再開するとき、user messageをdurable task contractへ反映する境界にだけ適用する。

## Durable requirement

- `## Amendments`へ永続化するのは、task固有の要求・制約・Acceptance criteria・既存Sol判断の意味を変更するsemantic deltaだけとする。
- semantic deltaは省略・要約で意味を落とさず、次のmodel call前にtask fileへ反映する。decision/fix本文だけでdurable requirementを差し替えない。
- 既にACTIVE task fileへ置いたsubstantive requirementを`USER_REQUEST`へ重複転記しない。`USER_REQUEST`はtask要旨・task file参照と、その呼出に必要なrun-controlだけに留める。

## Run control

- start/resume許可、現在task後の停止境界、runtime preflight、待機・進捗報告、親Codexのreview/Plan・History更新、最終報告形式は親orchestrationの実行状態であり、userがdurable task contractへ含めると明示した場合を除き`## Amendments`へコピーしない。
- ACTIVE taskの新規開始・再開前に既存glm-worker lifecycle stateから合法な次操作を判断する場合は、`glm-worker --handoff`の`consistent`・`required_action`・`allowed_actions`を正規入口とする。`consistent:false`では開始・再開可否を推測しない。repository lockやtask livenessの詳細診断が必要な場合だけ`--status`を追加で使う。
- run-controlだけのmessageではtask fileを変更しない。
- 1つのmessageにsemantic amendmentとrun-controlが混在する場合、semantic amendmentだけをlosslessに分離してtask fileへ反映し、message全体をamendmentとして転記しない。
- user messageの意味を判定するためのgeneric classifier・追加model callは作らない。親Codexが通常の要求解釈として境界を判断する。

## Execution milestone run control

- 1つのsemantic ACTIVE taskが運用上大きい場合だけ、親Codexは通常の要求判断として2〜8個のexecution milestoneへ分けてよい。token閾値・generic classifier・milestone判定専用model callは使わない。小さいtaskは従来どおり`glm-parent-action start`を使う。
- milestoneはtask fileへ追記するsemantic requirementではなくruntime execution authorityである。ACTIVE taskのContract/Must-not/Acceptanceを変更・複製せず、各milestoneには短い`id`、boundedな`scope`、そのunitの`acceptance`、必要な場合だけ`fresh_worker:true`を持たせる。
- milestone開始は通常start規則の明示例外として、sandbox内で`glm-parent-action prepare start-milestones`を1回実行し、返されたstaging fileのplaceholderだけを`apply_patch`で次のJSONへ置換する。token binding headerは保持する。`request`は固定要旨`現在のACTIVE taskを実行してください。`とし、ACTIVE task本文を複製しない。

```json
{"request":"現在のACTIVE taskを実行してください。","milestones":[{"id":"unit-a","scope":"bounded implementation responsibility","acceptance":"unit-a acceptance"},{"id":"unit-b","scope":"next bounded responsibility","acceptance":"unit-b acceptance","fresh_worker":true}]}
```

- 実行はsandbox外で`glm-parent-action start-milestones <token>`を1回だけ使う。wrapperはmilestone stateをdelegation前に保存し、その後のdecision・fix・approval・resumeは既存actionを使って同一task lifecycleを継続する。
- 実装途中でmaterially大きいと判明した場合はhealthyなin-flight callを止めない。`waiting-decision`、`waiting-sol-review`、rate limit、provider unavailable、recoverable guard、明示stop等の自然な停止境界だけで`glm-parent-action prepare revise-milestones`を使い、placeholderを`{"milestones":[...]}`へ置換してからsandbox外で`glm-parent-action revise-milestones <token>`を実行する。完了済みmilestoneと停止中in-flight milestoneは変更しない。
- milestone完了はtask完了ではない。最後のmilestone後も既存のtask-wide independent reviewer/Sol判断・validation・accept境界を維持する。次unitのfresh workerはcurrent Gitとdurable completion evidenceを正とし、新しいworkerになったことだけを理由に完了済みunitを再実装しない。明示semantic amendmentが完了済み範囲を変更した場合は、更新後task hashとcurrent milestoneのscope/acceptanceでそのdeltaを明示して扱う。

## 境界例

- `ACTIVE taskを開始し、完了したら次taskへ進まず結果を報告する`はrun-controlだけなのでtask fileを変更しない。
- `ACTIVE taskを開始する。production test selectionは導入しない、をMust notへ追加する`は後半だけをdurable requirementとしてtask fileへ反映し、開始・停止・報告手順は転記しない。
