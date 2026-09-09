# Task: Architecture responsibility and overdesign audit

## Original instruction

````text
# codex-worker-orchestrator 責務・過剰設計監査および改善

## 目的

`codex-worker-orchestrator`について、現在の本来の目的に対して、

* 過剰な設計になっていないか
* 過剰な実装を抱えていないか
* repository / `glm-worker` / repository harness が、本来持つ必要のない責務まで所有していないか
* correctness / safetyのために必要な機械化と、単に「親Codexにこう動いてほしい」という運用上の望ましい振る舞いの機械化が混同されていないか
* 1つのCLI / package / state machineへ責務が集まりすぎていないか

をcurrent treeから監査してください。

**監査だけで終了しないでください。**

監査結果に基づき、改善価値があるとCodex自身が判断したものは、

1. architecture / responsibilityを決定する
2. 必要なproduction変更を実装する
3. 必要なtestを追加・修正する
4. relevant / full validationを行う
5. Plan / Taskを現在の判断と一致させる

ところまで実行してください。

ただし、**何が過剰で、何を変更・削除・分離するべきかの最終判断はCodexが行ってください。**

この指示は特定機能の削除や特定architectureをあらかじめ要求するものではありません。

---

# 0. 作業原則

最初にcurrent authorityとcurrent implementationを確認してください。

少なくとも、

1. Git current tree
2. `README.md`
3. `IMPLEMENTATION_RULES.md`
4. `IMPLEMENTATION_PLAN.local.md`
5. current ACTIVE task
6. 関連production code
7. 関連test

を根拠にしてください。

過去の監査結果や、この指示内の懸念は**仮説**でありauthorityではありません。

current implementationで既に解消された問題を再度修正しないでください。

---

# 1. 前提: generic / repository-specific boundary

直前に`generic-repository-harness-boundary`が実装され、

* generic command harness
* このrepository固有のrepository harness / development policy

を分離する明示境界が追加されています。

したがって、

> repository固有Plan / Task / protected metadata / quality policy / self-protectionがgeneric `glm-worker`へ混在している

と過去の状態だけから決めつけないでください。

current implementationを確認し、

* 通常のforeign repository
* このrepository
* 偶然同名Plan / Task / Rules fileを持つforeign repository

の境界が正しく成立しているなら、その問題は解消済みとして扱ってください。

今回の主対象は、その次の問いです。

> **境界を正しく分けた後でも、その責務やmechanismそのものが本当に必要か。**

---

# 2. 最上位評価基準

repository authorityからcurrent objectiveを確認してください。

少なくとも以下の観点を使用してください。

* Codexをsemantic judgment / orchestrationへ集中させる
* 調査・実装・test・reviewをGLMへ安全に委譲する
* Sol High相当のQuality Deltaをできるだけ維持する
* Direct Codexと比較してCodex / Sol実消費を削減する
* stop / resume / provider failure等を安全に扱う
* mechanism自身のmaintenance / state / recovery / review costを含めた総コストを小さくする

単純なLOC削減は目的ではありません。

**少ない責務・明確なownership・低いstate complexityで同等以上の結果を出せるなら簡素化してください。**

---

# 3. core responsibilityを再定義する

current implementationから、このtoolのgeneric core responsibilityをCodex自身で定義してください。

候補例:

* model invocation
* worker / reviewer
* session
* lock
* lifecycle
* stop / resume
* rate limit / provider recovery
* packet
* Git snapshot / mutation safety
* parent handoff
* minimum telemetry

これは例示であり固定回答ではありません。

主要機能を少なくとも、

* generic core
* generic supporting mechanism
* repository-specific harness
* observability / analysis
* experiment / evaluation
* convenience
* questionable responsibility

へ分類してください。

---

# 4. control-plane complexityを監査する

以下の再帰が発生していないか確認してください。

```text
LLMがruleを破る
→ machine controlを追加
→ control用stateを追加
→ state inconsistencyが発生
→ recoveryを追加
→ recovery guardを追加
→ evidence / transaction / repair stateを追加
→ さらにcontrol-planeが巨大化
```

特に、

* control自身の不具合修正taskが増えていないか
* 一つのlogical stateを複数marker / file / fieldで持っていないか
* derived state間の同期不整合が障害原因になっていないか
* state transitionが複数writeの組合せになっていないか
* recovery machinery自身が新たなrecoveryを要求していないか

を確認してください。

現在または直近のlifecycle inconsistencyはarchitecture signalとしても評価してください。

### 改善方針

問題が確認された場合は、単発bugだけ直して終了せず、

* canonical stateの一本化
* derived state削減
* transaction境界整理
* state transition ownerの集約
* 不要marker削除
* recovery path削減

など、根本的にstate spaceを小さくできないか判断してください。

ただし、大規模rewriteが利益に見合わないなら行わないでください。

---

# 5. correctness invariant と workflow preferenceを分ける

machine enforcement対象を以下の2種類へ分けてください。

## A. correctness / safety invariant

違反すると、

* authority violation
* incorrect completion
* review bypass
* state corruption
* remote write violation
* unrecoverable task
* Quality Deltaの直接低下

などにつながるもの。

これはmachine enforcementの強い候補です。

## B. desirable workflow / parent behavior

例えば、

* 改善候補を忘れずTask化する
* 次の改善点を常に拾う
* 最適な順番で作業する
* user messageを適切に分類する
* 定期的に再評価する

など。

重要であっても、hard fail-closed lifecycle invariantにする必要があるとは限りません。

**「proseではLLMが守らない」だけを理由に、BをすべてAへ昇格させないでください。**

---

# 6. 重点対象を判断し、必要なら改善する

## 6.1 continuous-improvement-task-capture

current taskを読み、

* candidate生成
* durable state
* stable ID
* duplicate suppression
* semantic disposition
* Task / Plan exactly-once mutation
* unresolved candidateによるwait / completion / next-task blocking

までproduction machineryとして持つ必要があるか判断してください。

特に、

> 改善候補の取りこぼし

を

> correctness failure

と同じhard admission blockerにする妥当性を確認してください。

Codex判断により、

* current designを維持
* scope縮小
* soft telemetry / report化
* periodic auditへ移動
* existing task mechanismへ統合
* production implementationを行わない

のいずれでも構いません。

**必要ならtask自体を修正・統合・削除してください。**

---

## 6.2 user-requirement-ingress-binding

current taskを読み、

* user turn identity
* semantic requirement
* Amendment
* new task
* run-control
* question
* machine-visible ingress
* dispatch / completion blocking

をruntime responsibilityにする妥当性を判断してください。

特に、

> semantic classification自体は親Sol判断

であることを踏まえ、

machine stateを増やすことで本当にrequirement lossを防止できるのか確認してください。

機械保証できる部分が小さい場合は、巨大なstate machineで包まないでください。

必要ならtaskのscope変更・統合・削除を行ってください。

---

## 6.3 user-level installation scope

現在installer等が変更するuser-level surfaceを棚卸ししてください。

例:

* `~/.codex`
* Codex config
* Claude settings
* Git hook
* binary
* glm-worker state

以下を区別してください。

* user-owned global configuration
* repository-owned configuration
* tool-owned namespaced configuration/state
* installation artifact

repositoryが不必要にuser-global設定を所有しているなら改善してください。

既存`user-level-installation-scope-redesign`がownerなら重複taskを作らず、必要に応じてそのtaskを更新してください。

---

# 7. parent evidence / repo search / telemetryは効果で判断する

以下は複雑であること自体を削除理由にしないでください。

* parent evidence projection
* repository search
* telemetry
* bundle
* parent usage
* review gap
* convergence
* Codex reduction measurements

最上位目的のCodex Reductionに寄与している可能性が高いためです。

ただし、

> それを維持するためのparent orchestration / state / repair / model callが、削減できたtoken以上のcostを生んでいないか

を評価してください。

可能なものはexisting telemetry / bundle / eval evidenceで判断してください。

### 判断候補

* KEEP
* SIMPLIFY
* MEASURE FIRST
* MOVE TO OFFLINE TOOL
* REMOVE

---

# 8. CLI / command responsibilityを再設計してよい

現在の`glm-worker` CLIが、

* lifecycle mutation
* recovery
* status / inspection
* telemetry
* analysis
* evaluation
* experiment
* repository search
* bundle
* evidence

など多数の責務を持っていることを確認してください。

**command数が多いこと自体は問題ではありません。**

ただし、1つのCLI / parser / `CommandMode` / dispatchへ集約することで、

* unrelated featureの変更がcore runtimeへ影響する
* command structがunion-of-everythingになる
* lifecycleとoffline analysisが同じauthority surfaceになる
* test影響範囲が広がる
* app packageがGod object化する

のであれば、責務を分けてください。

### 分離は許可・推奨候補

必要なら例えば、

* `glm-worker`

  * model execution / lifecycleだけ
* `glm-worker-inspect`

  * status / handoff / timeline / stats等
* `glm-worker-analyze`

  * parent usage / review gap / convergence / outlier等
* `glm-worker-eval`

  * A/B / routing / test impact等

のようにcommandまたはbinaryを分けても構いません。

**この名称・分割をそのまま採用する必要はありません。**

Codexがcurrent dependency graphを確認し、

> 分けることでownership・dependency・test boundaryが明確になり、総complexityが下がる

場合だけ分離してください。

逆に、

> binaryが増えるだけで内部couplingが変わらない

なら分けないでください。

### 重要

CLI互換性のためだけに内部God architectureを残さないでください。

必要なら公開CLI互換wrapperを薄く残し、内部ownerを分離する設計も検討してください。

---

# 9. package responsibilityも改善してよい

`internal/`をinventoryし、

* core runtime
* repository-specific
* state
* analysis
* evaluation
* installation
* search
* parent evidence

などを分類してください。

packageが分かれていても実際には`app`や`workflow`へすべて集中している場合は、それも確認してください。

必要なら、

* command parser分離
* command handler分離
* read-only query service分離
* offline evaluator分離
* repository harness owner分離
* state transition owner集約

を行ってください。

「package数を増やす」ことを目的にしないでください。

---

# 10. planned taskをそのまま信じない

`IMPLEMENTATION_PLAN.local.md`の、

* ACTIVE
* NEXT
* BLOCKED / USER_PERMISSION_WAIT

を監査してください。

各taskについて、

* core correctness
* safety
* Quality Delta
* Codex Reduction
* repository cleanup
* observability
* experiment
* workflow preference
* control-plane expansion

を判断してください。

### 必要ならPlanを変更してよい

監査の結果、

* taskの前提が現在architectureと合わない
* taskを実装すると過剰なcontrol-plane expansionになる
* 他taskへ統合できる
* current implementationですでに解消済み
* より小さいmechanismで目的を達成できる

場合は、

* task scope修正
* task統合
* task削除
* priority変更
* 新しいcleanup/refactor task作成

を行ってください。

ただしduplicate taskを作らないでください。

---

# 11. prose-only-control-enforcement-auditへの追加条件

このtaskを実行する際は、

> prose-onlyだからmachine enforcementする

という判断を禁止します。

各controlについて先に以下を判定してください。

1. そもそもtoolが所有すべき責務か
2. correctness / safety invariantか
3. deterministicに観測可能か
4. deterministicにenforce可能か
5. machine state追加量はいくらか
6. failure / recovery pathはいくつ増えるか
7. enforcementによるbenefitがcomplexity taxを上回るか

このgateを通ったものだけmechanizeしてください。

通らないものは、

* proseとして残す
* observationだけmachine化
* offline auditへ移す
* responsibility自体を削除

などを選んでください。

---

# 12. 実装フェーズ

監査後、改善価値があるfindingをCodex自身で選択してください。

### 優先順位

原則として、

1. responsibility boundary violation
2. recurring state inconsistency
3. control-plane complexityを大きく減らせる変更
4. Codex Reductionを悪化させているmechanism
5. God CLI / package等のchange amplification
6. low-value convenience complexity

の順で考えてください。

ただしcurrent ACTIVE taskとの整合性は守ってください。

### 実装条件

* architecture判断を先に確定する
* unnecessary large rewriteを避ける
* current behaviorを意図せず破壊しない
* safety guaranteeを削る場合は、その保証自体が不要と判断した根拠を示す
* obsolete state / code / test / instructionを残さない
* compatibility shimを永久二重実装にしない
* relevant testsを追加する
* full validationを行う

---

# 13. 「追加」だけで改善しない

今回の目的はcontrolをさらに追加することではありません。

改善の結果として、

* code削除
* state削除
* command削除
* task削除
* instruction削減
* duplicate authority削除
* package責務分離

が適切なら実行してください。

特に、

> 新しいguardを追加して既存guardをそのまま残す

だけで問題を解決しないでください。

新しいarchitectureでobsoleteになったmechanismは除去してください。

---

# 14. 最終的に目指すarchitecture

Codex自身で、

> このtoolが目的を達成するためのminimum coherent architecture

を定義してください。

「minimum」は機能不足を意味しません。

以下を満たす最小構成です。

* GLMへ安全に委譲できる
* reviewできる
* failureから復旧できる
* Quality Deltaを維持できる
* Codex Reductionを測定・改善できる
* repository-specific policyがgeneric harnessへ漏れない
* control-plane自身の保守コストが支配的にならない

current architectureとの差を確認し、価値のある差分は今回実装してください。

大きすぎてcurrent taskへ安全に収まらない改善だけ、明確なTaskへ分離してください。

---

# 15. 完了条件

単なる監査reportでは完了しません。

以下を満たしてください。

## A. Audit

* current core purposeを定義済み
* responsibility map作成済み
* questionable complexityをevidence付きで判定済み
* planned taskを再評価済み

## B. Decision

各主要findingについてCodex自身が、

* KEEP
* NO CHANGE
* SIMPLIFY
* SPLIT
* MOVE
* REMOVE
* MEASURE FIRST
* DEFER WITH TASK

等の判断を行う。

## C. Implementation

今回実装可能かつ価値がある改善は実装済み。

特にarchitecture cleanupを単なるreportで先送りしない。

## D. Cleanup

変更後obsoleteになった、

* code
* state
* command
* test
* instruction
* Plan / Task

を整理済み。

## E. Validation

* relevant test
* full test
* lint / vet / build
* repository固有quality gate
* independent reviewer
* Sol semantic review
* 必要なruntime install / smoke

をcurrent authorityに従って完了する。

## F. Plan

Plan / ACTIVE / NEXT / BLOCKEDが、今回のarchitecture判断と矛盾していない。

---

# 16. 最終報告

最後に以下を報告してください。

## Core purpose

現在定義したgeneric core。

## Findings

主要findingとevidence。

## Decisions

各findingについて何を、

* 残した
* 簡素化した
* 分離した
* 移動した
* 削除した
* 計測待ちにした

か。

## Implementation

実際に変更したarchitecture / code / state / CLI / task。

## Removed complexity

削除できた、

* state
* path
* command
* duplicate authority
* control
* task

があれば明記。

## Remaining complexity

意図して残した複雑さと、その理由。

## Validation

実施したtest / review / runtime validation。

## Overall judgement

current treeが本来目的に対して、

* 適正
* 一部まだ過剰
* 大幅に過剰
* evidence不足

のどれかをCodex自身で判断する。

---

# Must not

* 過去の監査結論をそのまま採用しない
* 「大きいから削る」と判断しない
* 「安全そうだからcontrolを追加する」をデフォルトにしない
* audit reportだけ作って終了しない
* findingごとに無条件で新Taskを作らない
* current taskと重複するTaskを作らない
* commandを分けること自体を目的にしない
* backward compatibilityのために永久duplicate implementationを残さない
* semantic問題を「machine stateを追加したから解決」と扱わない
* Codex Reductionのためのmechanismを効果確認なしに削除しない
* correctness / safety invariantをcomplexity削減だけの理由で弱めない
````

## Amendments

none

## Resolved references

- 添付文書の内容はOriginal instructionへlosslessに保存済みであり、実行時に添付pathへ依存しない

## Purpose

current treeを本来目的に照らして監査し、correctness/safetyとworkflow preferenceを分離したminimum coherent architectureをCodexが決定する。改善価値のある範囲はproduction変更、test、validation、Plan/Task整理まで実施し、監査報告だけで終了しない。

## External feasibility

status: not-applicable

## Contract

- Git current tree、README、Rules、Plan、開始時のACTIVE task、関連production code/testを一次根拠にする
- generic core、generic supporting mechanism、repository-specific harness、observability/analysis、experiment/evaluation、convenience、questionable responsibilityの責務mapを作る
- control-plane再帰、derived state重複、transaction owner、recovery machinery、CLI/package change amplificationをcurrent evidenceで評価する
- correctness/safety invariantとdesirable workflow/parent behaviorを分離し、machine enforcementの責務・観測可能性・state/recovery costを判定する
- 指定されたplanned taskとPlan全体を再評価し、重複・前提消滅・過剰なcontrol-plane expansionがあればscope修正、統合、削除、priority変更を行う
- parent evidence、repo search、telemetry、bundle、usage/review/eval機構はCodex Reductionへの実効と総maintenance costからKEEP/SIMPLIFY/MEASURE FIRST/MOVE/REMOVEを判断する
- architecture/responsibility/API/state model等の意味判断はCodexが実装前に確定し、その判断内のrepository調査・実装・test・lint/build・自己reviewをGLMへ委譲する
- 今回安全に実装できて改善価値がある変更は実装し、obsolete code/state/command/test/instruction/taskを残さない
- 大きすぎる独立改善だけを具体的taskへ分離し、監査本体をreport-onlyで先送りしない
- relevant/full validation、repository quality gate、independent reviewer、Sol semantic review、必要なruntime install/smokeまで完了する
- 最終報告はCore purpose、Findings、Decisions、Implementation、Removed complexity、Remaining complexity、Validation、Overall judgementを含む

## Must not

- 過去の監査結論やOriginal instruction内の懸念をcurrent authorityより優先しない
- 大きさだけを削除理由にせず、control追加をdefault解決にしない
- audit reportだけで終了しない
- findingごとに無条件で新taskを作らず、current taskや既存planned taskと重複させない
- command/package分割自体を目的にしない
- backward compatibilityのために永久duplicate implementationを残さない
- semantic問題をmachine state追加だけで解決扱いにしない
- Codex Reduction機構を効果確認なしに削除しない
- correctness/safety invariantをcomplexity削減だけで弱めない
- GLM worker/reviewerへGit remote write authorityを与えない

## Acceptance criteria

- current core purposeとminimum coherent architectureが定義され、responsibility mapとevidence付きcomplexity評価がある
- Plan内の主要task、特にcontinuous-improvement-task-capture、user-requirement-ingress-binding、user-level-installation-scope-redesign、prose-only-control-enforcement-auditが再評価される
- 各主要findingにKEEP/NO CHANGE/SIMPLIFY/SPLIT/MOVE/REMOVE/MEASURE FIRST/DEFER WITH TASK等のCodex判断がある
- 今回実装可能で価値のあるarchitecture cleanupがproduction code/testへ反映され、obsolete surfaceが整理される
- relevant/full test、lint/vet/build、repository固有quality gate、independent reviewer、Sol semantic review、必要なinstall/smokeが成功する
- Plan、ACTIVE、NEXT、BLOCKEDが最終architecture判断と矛盾しない
- 最終報告がOriginal instructionの指定項目とoverall judgementを満たす

## Historical invariants

- parent-managed implementation metadataは親Codexだけが編集する
- generic repository harness boundaryがcurrent implementationで成立している場合は再実装しない
- GLM worker/reviewerにGit remote write authorityを与えない

## Dependencies

none

## Review findings

none

## Current boundary

実装前にcurrent tree監査とCodexによるarchitecture/責務判断を確定する。現ACTIVEの未完了diffへ本taskの変更を混在させない。
