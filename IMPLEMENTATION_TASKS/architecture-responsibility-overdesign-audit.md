# Task: Architecture responsibility and overdesign audit

## Original instruction

````text
# codex-worker-orchestrator のarchitecture / 責務を監査し、必要な改善を実装する

## このtaskの主目的

`codex-worker-orchestrator`のcurrent implementationを、本来の目的に対して監査してください。

確認したいのは主に、

* 過剰な設計・実装になっていないか
* `glm-worker`や周辺toolが、本来持たなくてよい責務まで持っていないか
* control-plane自身のためのstate / guard / recovery / evidenceが再帰的に増えすぎていないか
* correctness / safetyのための機械化と、単なる「親Codexの望ましい行動」の機械化が混ざっていないか
* CLI / package / state machineへ無関係な責務が集中していないか
* より小さく明確なarchitectureで同じ目的を達成できないか

です。

**このtaskの主成果はarchitectureの判断とcurrent production implementationの改善です。**

既存Taskの棚卸しやPlanの並べ替えは、その判断を既存backlogへ反映するための**副次工程**です。

---

# 1. 最上位目的を固定する

最初にcurrent Git tree、README、Rules、Plan、関連production code/testを確認し、このtoolの本来の目的をCodex自身で確定してください。

少なくとも現在の上位目的である、

* Codexをsemantic judgment / orchestrationへ集中させる
* 調査・実装・test・reviewをGLMへ安全に委譲する
* Sol High相当のQuality Deltaをできるだけ維持する
* Direct CodexよりCodex / Sol実消費を削減する
* 長時間実行、停止、resume、provider failureを安全に扱う

との関係でarchitectureを評価してください。

LOCや機能数の少なさ自体を目的にしないでください。

---

# 2. 作業優先順位

このtask内では次の優先順位を変えないでください。

```text
1. current production architectureの監査
2. architecture / responsibilityの判断
3. 改善価値のあるcurrent production implementationの改善
4. obsoleteになったcode/state/controlの削除
5. test / review / validation
6. 最後に既存TaskとPlanを新しいarchitecture判断へ同期
```

**6を1〜5の代わりにしてはいけません。**

既存Taskを整理したことだけでは、このtaskのarchitecture改善を実施したことにはなりません。

---

# 3. generic / repository-specific boundary

直前に導入されたgeneric command harnessとrepository-specific harnessの境界はcurrent implementationから再確認してください。

既に正しく、

* normal foreign repository
* このrepository
* 偶然同名のPlan / Task / Rulesを持つforeign repository

を分離できているなら、その問題を再度findingとして扱わないでください。

今回の問いは、

> **責務の置き場所が正しくなった後でも、その責務やmechanism自体が必要か**

です。

---

# 4. architecture監査対象

主要なproduction surfaceを実装から確認してください。

少なくとも以下を対象にします。

## Generic runtime

* model invocation
* worker / reviewer
* session
* lock
* lifecycle
* stop / resume
* rate-limit / provider recovery
* packet
* Git mutation / snapshot safety

## Control plane

* task status
* resume checkpoint
* pending decision / review等の補助state
* recovery
* quality surface handling
* parent action
* remote completion binding
* session rotation

## Context / observability

* parent evidence
* telemetry
* bundle
* repository search
* usage attribution

## CLI / app surface

* lifecycle mutation command
* recovery command
* inspection command
* analysis command
* eval / experiment command

## Installation / configuration

* user-global configuration
* repository-specific configuration
* tool-owned namespaced state/config

この一覧は結論を指定するものではありません。

Codex自身がcurrent dependency / ownershipを見て判断してください。

---

# 5. 特にcontrol-planeの自己増殖を確認する

以下のような再帰が起きていないか調査してください。

```text
LLMが期待どおり動かない
→ machine guardを追加
→ guard用stateを追加
→ state間の不整合が起きる
→ recoveryを追加
→ recovery用guard/evidenceを追加
→ さらにstate/recoveryが増える
```

特に、

* 一つのlogical stateを複数file / field / markerへ重複保持していないか
* derived state同士の同期がfailure sourceになっていないか
* state transitionが多数の独立writeの整合性に依存していないか
* recovery machinery自身のfailure recoveryが増えていないか
* orchestrator自身のcontrol-plane修復が開発作業の大きな割合を占めていないか

を確認してください。

実際の過去incidentをarchitecture signalとして利用して構いません。

---

# 6. correctness invariantとworkflow preferenceを分ける

機械化されている／予定されているcontrolについて、まず以下を区別してください。

## correctness / safety invariant

違反すると、

* authority violation
* state corruption
* review bypass
* incorrect completion
* unrecoverable task
* remote write violation
* Quality Deltaの直接的悪化

等になるもの。

これはhard machine enforcementが妥当な可能性があります。

## workflow preference / desirable parent behavior

例えば、

* 改善点を忘れずTask化する
* 良い順序でTaskを処理する
* user messageを必ず適切に分類する
* 定期的に改善候補を再評価する

等です。

こちらは重要でも、必ずしもproduction lifecycleをfail-closedさせるべき責務ではありません。

**「LLMはproseを守らない」という理由だけで、workflow preferenceをcorrectness invariantへ昇格させないでください。**

---

# 7. CLI / binary / packageは必要なら分ける

`glm-worker`へ無関係な責務が集中しているなら、commandやbinaryを分離することをarchitecture optionとして普通に検討してください。

例えば概念的には、

```text
runtime / lifecycle
inspection
analysis
evaluation
```

を別ownerにする可能性があります。

ただし、この分割を要求しているわけではありません。

判断基準は、

> 分割によってdependency、state ownership、change impact、test boundaryが実際に単純になるか

です。

単にbinary数が増えるだけなら分けないでください。

逆に、既存CLI互換性だけを理由に内部God objectを維持する必要もありません。

必要ならthin compatibility entrypointを残す判断も可能です。

---

# 8. 「複雑だが目的に必要」なものは残してよい

以下は複雑だからという理由だけで削除しないでください。

* telemetry
* parent evidence
* repo search
* bundle
* Codex usage analysis
* retry/recovery

Codex Reduction / Quality Deltaへ実際に寄与しているなら正当なcomplexityです。

判断するときは、

> このmechanismによる利益

と、

> このmechanism自身を維持するためのstate、model call、parent return、repair、test、review cost

を比較してください。

効果不明なら削除を即断せず`MEASURE FIRST`でも構いません。

---

# 9. architecture findingを実際に処理する

監査後、各主要findingについてCodexが、

* KEEP
* NO CHANGE
* SIMPLIFY
* SPLIT
* MOVE
* REMOVE
* MEASURE FIRST
* DEFER

等を判断してください。

名称は変更して構いません。

## 重要

**SIMPLIFY / SPLIT / MOVE / REMOVEと判断し、current task内で安全に実施可能なものは今回実装してください。**

一件だけ小さいcleanupを実施したからといって、それ以外の実装可能なarchitecture findingをすべて後続Taskへ送ってtaskを終了しないでください。

今回見つかったfindingはそれぞれ、

1. 今回実装する
2. NO CHANGE
3. MEASURE FIRST
4. 今回実装できない具体的理由があるのでDEFER

のどれかまで判断してください。

---

# 10. DEFERには具体的理由が必要

以下はDEFER理由になりません。

* 既存Taskがある
* Planに後続Taskとして載っている
* 今回の監査で見つけたのでTask化した
* scopeがそこそこ大きい

DEFERしてよいのは例えば、

* current architecture変更と独立しており、同時変更するとatomicityを壊す
* 外部API / 実測 / user permissionが必要
* current evidenceではKEEPかREMOVEか判断不能
* 安全なmigrationを別単位で行う必要がある
* current taskへ混在させることで明確にriskが増える

場合です。

その理由をfindingごとに残してください。

---

# 11. 既存Task棚卸しの位置付け

architecture判断が終わった後で、`IMPLEMENTATION_PLAN.local.md`のACTIVE / NEXT / BLOCKEDと既存Taskを確認してください。

目的は、

> **今回決めたarchitectureとbacklogを矛盾させないこと**

です。

各Taskについて必要なら、

* 維持
* scope修正
* 統合
* 削除
* priority変更
* BLOCKED移動

を行って構いません。

ただし、

**既存Task全件の分類・並べ替えそれ自体を今回のarchitecture改善成果として扱わないでください。**

Task棚卸しはarchitecture decisionの**出力先**であって、architecture decisionの代替ではありません。

---

# 12. 重点確認する既存Task

少なくとも以下はcurrent architecture判断と照合してください。

* `continuous-improvement-task-capture`
* `user-requirement-ingress-binding`
* `prose-only-control-enforcement-audit`
* `user-level-installation-scope-redesign`

特に、

> workflow preferenceまでhard production state machineへ拡張しようとしていないか

を確認してください。

ただし、削除ありきでは判断しないでください。

---

# 13. 改善では「追加」だけでなく削除も行う

architecture改善として新しいguard / abstraction / stateを追加する場合は、

> その結果不要になる既存mechanismは何か

も必ず確認してください。

改善後obsoleteになった、

* state
* marker
* recovery path
* command
* parser branch
* duplicated authority
* instruction
* test fixture
* package dependency

は削除してください。

**guardを追加して古いguardも全部残す、という方向をdefaultにしないでください。**

---

# 14. 完了判定

このtaskは、次を全部満たしたときだけ完了です。

## Architecture

* generic core responsibilityを定義した
* major production surfaceを責務分類した
* control-plane complexityを評価した
* minimum coherent architectureをCodex自身で定義した

## Decision

主要findingすべてについて、

* change
* no change
* measure first
* defer

の判断がある。

## Implementation

Codexが「改善すべき」と判断し、今回安全に実施可能なarchitecture変更は実装済み。

**既存Taskへ移しただけのfindingを「実装済み」と数えない。**

## Cleanup

変更後obsoleteになったproduction surfaceを除去済み。

## Validation

current authorityに従って、

* relevant/full test
* lint/vet/build
* quality gate
* independent review
* Sol semantic review
* 必要なruntime install/smoke

を完了済み。

## Backlog sync

最後にPlan / Taskをarchitecture判断へ同期済み。

---

# 15. production変更がゼロでも完了できる例外

監査の結果、

> current production architectureには今変更すべき点がない

とCodexが本当に判断する場合、production diffゼロでも構いません。

ただしこの場合、

* CLI
* state model
* control-plane
* parent evidence
* repo search
* analysis/eval
* repository harness
* installer ownership

等の主要surfaceについて、current codeを根拠に`NO CHANGE`または`MEASURE FIRST`を説明してください。

**「既存Taskで後で対応する」はproduction変更ゼロの根拠になりません。**

---

# 16. 最終報告

最後に以下を簡潔に報告してください。

### Core

このtoolのgeneric coreをどう定義したか。

### Architecture findings

何が過剰／妥当／要計測だったか。

### Changes

今回current production implementationを何を、

* simplified
* split
* moved
* removed

したか。

### Removed complexity

削除できたstate / control / command / authority等。

### No-change decisions

疑ったが現状維持としたものと理由。

### Deferred findings

今回実装しなかったものと、**既存Taskの存在以外の具体的理由**。

### Backlog changes

architecture判断の結果として最後に変更したTask / Plan。

### Overall judgement

current architectureが本来目的に対して、

* 適正
* 一部過剰
* 明確に過剰
* evidence不足

のどれに当たるか。

---

# Must not

* Task棚卸しを主目的へ昇格させない
* Plan再優先付けだけで完了しない
* 一件だけ小さなcleanupを実施し、残りの実装可能findingをすべてbacklogへ送って完了しない
* 「既存Taskがある」をDEFER理由にしない
* audit checklistを埋めただけでarchitecture goal達成と扱わない
* command数やLOCだけを理由に削除しない
* control追加をdefault解決にしない
* semantic workflow preferenceを無条件にhard state machine化しない
* correctness / safetyをcomplexity削減だけの理由で弱めない
* generic/repository harness boundaryが既に成立しているなら再実装しない
* GLM worker/reviewerへGit remote write authorityを与えない
````

## Amendments

none

## Resolved references

- 2026-09-10の刷新添付本文はOriginal instructionへlosslessに保存済みであり、実行時に添付pathへ依存しない
- 置換前のOriginal instructionはGit履歴 `af68f3f:IMPLEMENTATION_TASKS/architecture-responsibility-overdesign-audit.md`から回収可能だが、今後の実行要求には使用しない

## Purpose

current production architectureの責務・過剰設計・control-plane自己増殖を本来目的に照らして監査し、minimum coherent architectureと主要findingのdispositionをCodexが確定する。今回安全に実施可能なarchitecture改善、obsolete production surfaceの削除、test・review・validationを完了してから、最後にbacklogを判断結果へ同期する。Task棚卸しやPlan再配置は主成果として数えない。

## External feasibility

status: not-applicable

## Contract

- Git current tree、README、Rules、Plan、開始時のACTIVE task、関連production code/testを一次根拠にする
- 作業順序をcurrent production architecture監査、architecture/responsibility判断、実装可能なproduction改善、obsolete code/state/control削除、test/review/validation、最後のTask/Plan同期に固定し、backlog整理を先行成果にしない
- generic core、generic supporting mechanism、repository-specific harness、observability/analysis、experiment/evaluation、convenience、questionable responsibilityの責務mapを作る
- generic runtime、control plane、context/observability、CLI/app、installation/configurationの主要production surfaceをcurrent dependency・state owner・change impactから監査する
- control-plane再帰、derived state重複、transaction owner、recovery machinery、CLI/package change amplificationをcurrent evidenceで評価する
- correctness/safety invariantとdesirable workflow/parent behaviorを分離し、machine enforcementの責務・観測可能性・state/recovery costを判定する
- 指定されたplanned taskとPlan全体を再評価し、重複・前提消滅・過剰なcontrol-plane expansionがあればscope修正、統合、削除、priority変更を行う
- parent evidence、repo search、telemetry、bundle、usage/review/eval機構はCodex Reductionへの実効と総maintenance costからKEEP/SIMPLIFY/MEASURE FIRST/MOVE/REMOVEを判断する
- architecture/responsibility/API/state model等の意味判断はCodexが実装前に確定し、その判断内のrepository調査・実装・test・lint/build・自己reviewをGLMへ委譲する
- 今回安全に実装できて改善価値がある変更は実装し、obsolete code/state/command/test/instruction/taskを残さない
- 一件のnarrow cleanupだけで終了せず、主要findingごとに今回実装、NO CHANGE、MEASURE FIRST、具体的理由付きDEFERのいずれかを確定する
- DEFERは独立atomicity、外部API/実測/user permission、evidence不足、安全なmigration分離等の具体的理由を必要とし、既存Taskの存在、Plan掲載、scopeの大きさだけを理由にしない
- production変更ゼロと判断する場合は、CLI、state model、control plane、parent evidence、repo search、analysis/eval、repository harness、installer ownershipの各主要surfaceへcurrent code根拠付きNO CHANGEまたはMEASURE FIRST判断を示す
- Task棚卸しとPlan再優先付けはarchitecture decisionの出力先として最後に行い、それ自体をarchitecture改善成果へ数えない
- relevant/full validation、repository quality gate、independent reviewer、Sol semantic review、必要なruntime install/smokeまで完了する
- 最終報告はCore、Architecture findings、Changes、Removed complexity、No-change decisions、具体的理由付きDeferred findings、Backlog changes、Overall judgementを含む

## Must not

- 過去の監査結論やOriginal instruction内の懸念をcurrent authorityより優先しない
- 大きさだけを削除理由にせず、control追加をdefault解決にしない
- audit reportだけで終了しない
- Task棚卸しやPlan再優先付けだけで完了しない
- 一件だけ小さいcleanupを実装し、残る実装可能findingをすべてbacklogへ送って完了しない
- 「既存Taskがある」「Planへ載せた」「scopeが大きい」だけをDEFER理由にしない
- findingごとに無条件で新taskを作らず、current taskや既存planned taskと重複させない
- command/package分割自体を目的にしない
- backward compatibilityのために永久duplicate implementationを残さない
- semantic問題をmachine state追加だけで解決扱いにしない
- Codex Reduction機構を効果確認なしに削除しない
- correctness/safety invariantをcomplexity削減だけで弱めない
- GLM worker/reviewerへGit remote write authorityを与えない

## Acceptance criteria

- current core purposeとminimum coherent architectureが定義され、responsibility mapとevidence付きcomplexity評価がある
- generic runtime、control plane、context/observability、CLI/app、installation/configurationの主要surfaceがcurrent production codeから監査されている
- Plan内の主要task、特にcontinuous-improvement-task-capture、user-requirement-ingress-binding、user-level-installation-scope-redesign、prose-only-control-enforcement-auditが再評価される
- 各主要findingにKEEP/NO CHANGE/SIMPLIFY/SPLIT/MOVE/REMOVE/MEASURE FIRST/DEFER WITH TASK等のCodex判断がある
- 今回実装可能で価値のあるarchitecture cleanupが全件production code/testへ反映され、変更後obsoleteになったstate/control/command/authority/instruction/test fixture/package dependencyが整理される
- 未実装findingには既存Taskの存在以外の具体的DEFER理由、またはevidence根拠付きNO CHANGE/MEASURE FIRST判断がある
- task inventory、Plan再配置、個別taskの統合・削除、および一件だけのnarrow cleanupを単独完了根拠にしていない
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

未完了。過去roundの全Task棚卸し・Plan再編・Plan branch derived-state削除は入力evidenceおよび一findingのcleanupとして再利用できるが、repository全体のarchitecture/responsibility監査の完了根拠にはしない。現在ACTIVEのtask完了後にNEXTとして開始し、最新Amendmentの作業順序と完了条件で改めて実行する。

