# Task: detected defect task registration enforcement

## Original instruction

````text
それは不具合なの？不具合ならタスクとして起票するんじゃないの？
お前がやったことは2個問題がある
不具合をタスクとして起票しなかったこと
「不具合をタスクとして起票する」というルールを守らなかったこと
これら2件を起票しろ
````

## Amendments

### 2026-09-18

````text
もう一つあるだろ
起票するのがルールなのに起票しなかったってバグが
````

## Resolved references

- parent Codexはcontinuation metadata guardがGOALなしPlanの完了同期を拒否する不具合を確認し、原因と影響をユーザーへ報告したが、対応するtask fileとPlan entryを作成せず停止した
- ユーザー指摘後に初めて不具合taskを起票したため、既存の「新規Findingを現在taskへ混ぜず独立taskとしてPlanへ追加する」規則がparent behaviorとして守られなかった
- parent Codexはpublication finalizationの手順誤りとmachine enforcement欠落をverified defectとして説明した後も、修復のためのGit履歴操作についてユーザー許可を求めて停止し、対応taskを同じturnで登録しなかった
- 上記publication incidentは既存task登録漏れと同じ原因層の再発であり、ユーザー指摘後に初めて `publication-finalization-machine-enforcement.md` を登録した
- natural languageだけから任意の不具合を完全検出することはmachine gateで保証できないため、検出済み・分類済みのFindingをdurable taskへ結び付ける境界を対象とする
- current treeにはpublication invalidation専用の`record-publication-finding` / `PublicationInvalidatingFinding`は存在するが、current-task外のgeneric verified defectをtask registration待ちとして表すcanonical state/actionは存在しない
- continuation stop/completion gateはmachine-readable stateからしか未登録Findingを判定できないため、semantic adoptionとtask+Plan登録完了の間を表すbounded machine recordが本taskのfail-closed acceptanceに必要である
- runtime recordへdefect本文を複製せず、semantic task pathをFinding identity兼locatorとして保持すれば、task fileを唯一のlossless本文正本に維持できる

## Purpose

親Codexが検出・報告済みの独立不具合をconversationだけに残して停止することを防ぎ、実装開始前にtask fileとPlanへlosslessに登録させる。

## External feasibility

status: not-applicable

## Contract

- parentが現在task外の不具合をverified findingとして採用した時点で、同じturnの終了、ユーザーへの許可要求、または次の長時間処理より前にsemantic task fileとPlan entryへ結び付ける正規surfaceを定義する
- defectの一次指示、観測事実、影響、原因が未確定ならunknownであること、再現・acceptance boundaryをlossless sourceとderived contractへ分離して保存する
- task登録前のparent stop / completion / handoffが、登録待ちFindingの存在をmachine-readableに検出できる範囲ではfail closedする
- natural language classifierや追加model callへ一般的な不具合検出を委ねず、親が意味判断した後のregistration lifecycleをdeterministicに強制する
- current ACTIVEへ混在させず、priorityと割り込み要否はPlanで管理する
- public CLI、state / checkpoint、parent action、停止admissionへ新しいcontractが必要な場合は実装前に`NEEDS_SOL_DECISION`で確定する

## Sol decision authority

- responsibility: verified defectかどうか、semantic task path、task本文、Plan priority/interrupt判断はparentが所有し、machineはparentが採用を記録した後のpending registration identity、task+Plan binding検証、未登録中のparent action/stop/completion fail-closedだけを所有する
- dependency-direction: parent action layerがrecord/bindを実行し、stateはbounded pending registration recordだけを永続化する。task/Planの構文・schedule membership検証は既存taskcontract/repository project側を再利用し、stateへrepository Markdown parsing責務を逆流させない
- public-surface: parent-only `glm-parent-action record-defect-finding --task <IMPLEMENTATION_TASKS/...md>` と `glm-parent-action bind-defect-task --task <same-path>` を正規surfaceとする。record後はcanonical handoffが`bind-defect-task`をrequired actionとしてtask path parameter付きで投影し、generic natural-language defect detectionは追加しない
- compatibility: pending registration stateが存在しないrepositoryの既存behaviorは変更せず、machine-only stateはcurrent schemaだけを受理する。legacy alias、schema migration、旧state推定fallbackは追加しない
- validation-error-semantics: malformed/unreadable state、Finding identity/task path不一致、source ACTIVE binding不整合、task fileまたはPlan membership欠落、異なるpending identityとの衝突はfail closedする。同じsemantic task pathの再recordはidempotentとし、既にtask+Planへ登録済みならduplicateとして新しいtaskを要求しない

## Must not

- 未確認の違和感、意図どおりの安全停止、単発の外部limitをすべて自動task化しない
- defect本文をHistoryやruntime stateへ第二正本として複製しない
- task file生成をGLM worker/reviewerへ許可しない
- conversation summaryや親の記憶だけを登録済み証拠にしない
- task登録を理由に現在ACTIVEを自動で切り替えたり、新taskを自動開始しない
- generic natural language classifierや専用model callを追加しない

## Acceptance criteria

- parentがcurrent task外のverified defectを採用したscenarioで、task fileとPlan entryがないまま停止・完了へ進めないことをintegration testで固定する
- task fileとPlan entryが同一Finding identityへ結び付いた後は、現在ACTIVEを維持したまま親処理を継続または停止できる
- duplicate reportは同一taskへ収束し、同じ不具合のtask fileを重複生成しない
- rejected / unverified / current-task内findingは独立task registrationを要求しない
- parent-managed metadata integrity、task filename規則、schedule closure、workerによるmetadata編集禁止を維持する
- 今回のcontinuation metadata guard不具合を検出した再現scenarioで、未起票のままfinal responseへ進む経路を防止する
- publication finalization欠陥をverified findingとして説明した後、Git recoveryの許可要求へ進むscenarioでも、task fileとPlan entryがないままfinal responseへ進めない

## Historical invariants

- 新規ユーザー要求と採用済みFindingはconversationだけに保持したまま長時間処理・実装・停止へ進めない
- 独立不具合は現在ACTIVEへ混在させず、semantic taskとしてPlan lifecycleへ登録する
- taskの意味判断とpriorityは親Codex、実装とreviewはGLMが担う

## Dependencies

none
