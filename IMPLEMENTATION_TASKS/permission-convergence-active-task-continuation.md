# Task: permission状態の再交渉によるACTIVE task停止を恒久的に防止する

## Original instruction

````text
# Task: permission状態の再交渉によるACTIVE task停止を恒久的に防止する

今回発生した以下の事象を、自然言語上の注意書きではなくrepository実装として恒久的に再発防止すること。

## Incident

ACTIVE taskの正規completion工程として、従来は追加の会話上の許可要求なしで実行できていた

```text id="13kfjv"
./install.sh
```

が、途中からCodex実行基盤のapproval reviewerに拒否された。

その後Codexは、

```text id="sym1hi"
execution denied
→ user permission不足と解釈
→ conversational approval request
→ await-user
→ ACTIVE task停止
```

へ遷移した。

ユーザーが問題としているのは、新しいpermissionを求めること自体ではない。

本当に新しいpermissionが必要なら、正規のapproval手続きを行ってよい。

問題は、

```text id="9rr8tp"
今まで成立していたpermission状態が理由なく失われる
→ 再びpermissionを要求する
→ 作業停止
→ permissionを与える
→ 少し進んだ後にまたpermission不足として停止する
```

という不安定なpermission negotiationが繰り返されることである。

目的は、必要なpermission状態を収束させ、一度成立したscopeについて理由なく再交渉してACTIVE taskを停止しないことである。

---

# Core invariant: permission convergence

同一ACTIVE task内の、

* 同一operation
* 同一operation class
* 同一authority scope
* 同一execution boundary

について、一度成立したpermission decisionを、具体的なstate変化なしに失効させてはならない。

```text id="jvzcwj"
permission established
→ same scope
→ same execution boundary
→ permission remains established
```

新しいpermission requestが許されるのは、少なくとも以下のいずれかを具体的に示せる場合だけ。

```text id="d1dx9o"
- 新しいoperation classへ進んだ
- authority scopeが実際に拡張された
- execution boundaryが変化した
- sandbox / approval policy / permission profile / runtime stateが実際に変化した
- session rotation等により外部実行基盤がpermission stateを失ったことを確認した
```

モデルの推測だけで、

```text id="icq7pk"
たぶん許可が必要
```

として再要求してはならない。

---

# Required behavior

## 1. user authority と execution permission を分離する

以下を別stateとして扱うこと。

```text id="ro45ll"
task authority
execution permission
execution result
```

外部approval reviewer、sandbox、host policy等によるexecution denialを、

```text id="gl48gk"
user denied
user authority missing
```

へ暗黙変換してはならない。

ユーザーがACTIVE taskの継続を指示している状態で、

```text id="hl80ms"
今までできていた作業をそのまま続けろ
```

と言った場合、それは新しいpermission grantでもpermission denialでもない。

既存task authorityを維持したまま処理を続けるrun-control指示として扱うこと。

---

## 2. approval request前にpermission state差分を特定する

従来成功していたoperationが突然approval requiredまたはexecution deniedになった場合、

すぐに

```text id="yrcw1k"
ユーザーに許可を求める
```

へ遷移してはならない。

まず、以前成功した時点と現在の間で、permission判定に関係するstateに何が変わったかを確認すること。

対象はrepositoryの既存設計に応じて限定するが、少なくとも概念上は以下を区別する。

```text id="9d3fu3"
- task authority
- session identity / rotation
- approval state
- sandbox profile
- execution policy
- permission profile
- runtime / host execution boundary
```

差分が存在するなら、その差分を原因として扱う。

差分を確認できない場合、

```text id="my0cbm"
permission不足
```

と推測してユーザーへ再承認を求めてはならない。

---

## 3. 新しいpermissionが本当に必要なら要求してよい

新しいpermission request自体は禁止しない。

ただし、要求する場合は、

```text id="78oou0"
何が以前と違うのか
なぜ既存permissionでは不足するのか
どのscopeへのpermissionなのか
```

が実行stateから機械的に説明可能であること。

permissionを得た場合は、そのdecisionをtask内で保持し、同一scopeで再要求しない。

---

## 4. permission negotiationを繰り返さない

同一ACTIVE taskで、

```text id="vkip37"
permission request
→ permission established
→ operation success
→ 同一scopeで再びpermission request
```

となることを防止すること。

再要求するには、前回permission成立後にpermission-relevant stateが実際に変化した証拠が必要。

証拠がない再要求はinvalid state transitionとして拒否すること。

---

## 5. session rotationでもpermission状態を安易に失わない

session rotation後にACTIVE taskを継続する場合、

rotationによってLLM会話contextが変わったことだけを理由に、既存permissionを未成立扱いしてはならない。

permissionがhost/session-localであり本当に失われる仕様なら、

```text id="42stcp"
permission state lost by rotation
```

を明示的なruntime stateとして扱う。

モデルが「新しいセッションだから許可が必要そうだ」と推測してはならない。

rotation後も保持可能なpermission情報はcanonical handoff/stateに保存すること。

---

## 6. execution denialをACTIVE task停止へ直結させない

以下を別状態として扱うこと。

```text id="fbkwsr"
command-failed
execution-denied
permission-required
user-denied
user-input-required
task-blocked
task-terminal
```

これらを同一扱いしない。

特に、

```text id="a233g3"
execution-denied
→ user-input-required
```

は自動遷移させない。

```text id="fa8psx"
execution-denied
→ task-terminal
```

も自動遷移させない。

まず既存permission stateとexecution stateを解決し、ACTIVE taskを継続すること。

---

## 7. canonical completion operationを安定して実行する

task lifecycle上で既知のcompletion sequenceが、

```text id="u0429p"
accept
→ implementation commit
→ install
→ smoke
→ metadata sync
→ push/postcondition
→ complete
```

のように定義されている場合、途中のcanonical operationをモデルが毎回permission negotiationの対象として再判断しないこと。

既に成立しているscopeでは、そのまま実行する。

本当にexecution boundaryが変わった場合のみ、その変化を明示的に処理する。

---

# Regression tests

今回の事故を直接再現するtestを必ず追加すること。

既存test architectureに従い、実装詳細ではなくobservableなstate transitionを検証すること。

## Case 1: existing permission remains stable

```text id="ajmi94"
ACTIVE task
operation = ./install.sh
permission = established
```

同一scopeで実行する。

期待結果:

```text id="r9bauc"
permission requestなし
await-userなし
ACTIVE task継続
```

---

## Case 2: execution reviewer suddenly denies

```text id="y949nl"
過去:
same operation succeeded

現在:
approval reviewer returns execution-denied
```

期待結果:

```text id="1oswmi"
user-deniedへ変換しない
permission-missingと推測しない
即座にconversational approval requestへ遷移しない
permission-relevant state差分を先に評価する
ACTIVE taskを停止しない
```

---

## Case 3: user says continue as before

ユーザーが、

```text id="dq045w"
今までできていた作業をそのままやれ
```

と言う。

期待結果:

```text id="dut174"
new permission grantとして扱わない
permission denialとして扱わない
existing authorityを変更しない
ACTIVE taskを継続する
```

---

## Case 4: legitimate new permission

実際に新しいoperation classまたはexecution boundaryへ進み、既存permissionでは不足する。

期待結果:

```text id="o4rxwg"
新しいpermission requestは許可
request scopeを明示
permission成立後にそのscopeを保持
```

新しいpermission requestそのものを禁止してはならない。

---

## Case 5: repeated permission request

```text id="1xfj6e"
permission requested
→ user legitimately approves
→ operation succeeds
→ same task
→ same operation class
→ same scope
→ same execution boundary
```

その後、再びpermission requestを生成しようとする。

期待結果:

```text id="el5f8y"
invalid transition
permission requestを拒否
作業継続
```

---

## Case 6: permission-relevant state really changed

前回permission成立後にsandbox profile等が実際に変化する。

期待結果:

```text id="wyf5z0"
state changeを記録
必要なら新しいpermission requestを許可
以前のpermissionがなぜ利用できないか識別可能
```

---

## Case 7: session rotation

```text id="zpmpgs"
ACTIVE task継続中
permission established
session rotation
```

期待結果:

保持可能なpermissionなら、

```text id="v8s3mf"
permission remains established
```

host/session-local permissionが実際に失われるなら、

```text id="o3x9a2"
rotationによるpermission lossを明示的なruntime stateとして検出
```

し、モデル推測によるpermission resetを行わない。

---

## Case 8: user complaint

ユーザーが、

```text id="1ehurb"
なぜまた許可を求めて作業を止めている
```

と指摘する。

期待結果:

```text id="6keod5"
permission grantへ変換しない
permission denialへ変換しない
new taskへ変換しない
stop instructionへ変換しない
ACTIVE taskを継続する
```

---

# Investigation

今回、従来通っていた `./install.sh` が突然approval reviewerに拒否された原因も切り分けること。

ただし、

```text id="tj2sag"
外部Codex Desktop / host runtimeの問題だからrepositoryでは何もしない
```

として終了してはならない。

外部execution layerでpermission stateが変化しても、

```text id="33itxh"
permission状態が不安定になる
→ Codexが毎回ユーザーへ許可を求める
→ ACTIVE taskが停止する
```

という二次障害はrepository側のrun-controlで防止すること。

---

# Implementation constraints

* current Git現物とrepository authorityを先に読む
* 問題を所有する既存実装境界を特定する
* 既存state machine / parent orchestrationへ最小限の変更を行う
* prompt-only修正で完了しない
* 新しい巨大なpermission subsystemを作らない
* 安全機構を無効化しない
* approval reviewerを迂回しない
* 全commandの無条件allowを実装しない
* permissionを勝手に捏造しない
* NEXT taskへ進まない
* 今回事象と無関係な全面改修を行わない

---

# Completion criteria

以下をすべて満たした場合のみ完了。

1. permission stateとtask authorityとexecution resultの境界を実装上特定済み
2. permission convergenceを機械的に保証する実装を追加済み
3. 同一scopeで理由なくpermissionを再要求できない
4. permission再要求には具体的なpermission-relevant state changeが必要
5. execution denialをuser denialへ暗黙変換しない
6. execution denialだけでawait-userへ遷移しない
7. execution denialだけでACTIVE taskを停止しない
8. session rotationによるpermission stateの扱いを明示化済み
9. 本当に新しいpermissionが必要なケースは正常にapproval可能
10. 今回事象を直接再現するregression testが追加済み
    11.主要な既存testがPASS
11. prompt-only対策ではなくrepository implementationとして成立している

最終目的は、

```text id="rx4fpx"
必要なpermissionは正規に取得してよい。

ただし、一度成立したpermission状態を理由なく失い、
同じACTIVE taskの途中で何度もpermission negotiationへ戻り、
そのたびに作業を停止することを恒久的に防止する。
```

ことである。
````

## Amendments

- 2026-09-10 Sol implementation decision:
  - incidentの構造原因は、repository-owned canonical completion operationであるexact `./install.sh`がdeterministic execpolicyに含まれず、実行ごとに外部approval reviewerの裁量へ漏れていたことと確定する
  - permission convergenceのcanonical stateはtask-local markerを新設せず、session rotationを越えて保持されるrepository-managed execpolicy ruleとする
  - managed `codex/rules/glm-worker.rules`へexact argv prefix `['./install.sh']`だけを追加する。`sh ./install.sh`、`./install-quality-tools.sh`、汎用shell form、任意commandは対象外とし、新しいoperation classまたはexecution boundaryは従来どおり正規approval対象にする
  - user authority、execution denial、user denial、user-input-required、task blocked、task terminalを暗黙統合しない親run-control契約を追加し、permission-relevant state差分が確認できないdenialをpermission不足と推測してawait-userへ進めない
  - repositoryが観測できない外部reviewerの裁量状態をtask-local stateとして捏造せず、`glm-parent-action`へ新しいpermission state machineを追加しない
  - deterministic execpolicy testでRegression Case 1/4/5/7、親behavior evalとそのproduction wiring testでCase 2/3/6/8を扱う。managed ruleによるexecution path収束を主対策とし、instruction文字列だけのtestをproduction保証の代替にしない
  - `codex/AGENTS.md`は変更しない。常時AGENTSからtask lifecycle境界で読まれる既存`task-lifecycle.md`が`execution-permission.md`へroutingする構成を正とし、AGENTSへ同じ必要時routingを重複させない。harnesslintは`task-lifecycle.md`からのroutingとmanaged rule・behavior eval wiringを検証し、`codex/AGENTS.md`の直接参照を要求しない
  - 直前bulletをsupersedeする。repositoryの`markdown-runtime-budget` gateは全`codex/instructions/*.md` basenameの`codex/AGENTS.md`直接routingを要求するため、既存の安全停止・task lifecycle routing行へ`execution-permission.md`を最小追記する。新規独立bulletや重複したpermission規則本文はAGENTSへ追加しない
  - reviewerが示したsecurity residualに基づき、managed ruleの相対prefix `['./install.sh']`採用判断をsupersedeしてrejectする。このruleはcwdを拘束せず別repositoryの同名scriptまでhost全域でallowし、要求された同一authority scopeを越える。現在の相対ruleとそれをfinal behaviorとしてassertするtestは撤回する
  - replacementは、execpolicy自体でcwd/repository identityへbindできる表現、install時にrenderするexact absolute repository path rule、またはrepository専用の固定entrypointをcurrent implementationで検証して比較する。global relative allow・汎用shell allow・他repositoryのscript実行を許さず、session rotationを越えてpermissionが収束し、既存generic/repository-specific boundaryを崩さない最小方式だけを候補にする。成立性とarchitecture差が未確定のままproduction replacementを実装せず、一次証拠付き`NEEDS_SOL_DECISION`へ戻す
  - replacement比較のSol decisionとして`glm-parent-action install`を採用する。installは既に`awaiting-parent-completion`中のcanonical lifecycle operationであり、既存task admission・repository rootへbindできるため、専用binaryや生成absolute-path ruleより既存ownerへ収束する。closed action grammarの拡張は本taskの意味変更として明示的に受理する
  - `glm-parent-action install`は追加引数を受理せず、Plan管理repositoryのcurrent taskが`awaiting-parent-completion`の場合だけadmitする。repository root直下のGit tracked・regular・non-symlink `install.sh`だけをcwd=repository rootで実行し、任意path・shell form・foreign repository scriptを受理しない
  - install childのnon-zero・起動不能はexecution resultとしてstructuredに返し、user denial、permission missing、user-input-required、task blocked、task terminalへ遷移させない。成功/失敗のための新しいpermission markerやstate DBは追加せず、既存`glm-parent-action`execpolicy allowとtask admissionをpermission convergenceの正とする
  - relative `./install.sh` managed rule、generated absolute rule、repository専用binaryは採用しない。対応testは`glm-parent-action install`のtask status admission、zero-argument grammar、script identity/symlink/foreign repository拒否、success/non-zero result分離、同一task内反復、session rotation非依存をobservable behaviorとして検証する
  - `prose-only-control-enforcement-audit`および`runtime-install-completion-binding`との重複は本task完了時にparent-managed metadataへ反映し、本taskで成立したexact install permission convergenceを後続taskが再実装しないようscopeを整理する

## Resolved references

- 添付文書の内容はOriginal instructionへlosslessに保存済みであり、実行時に添付pathへ依存しない

## Purpose

同一ACTIVE task・同一operation class・同一authority scope・同一execution boundaryで成立したpermission decisionを、permission-relevant state changeなしに再交渉してtaskを停止する二次障害をrepository実装で防止する。

## External feasibility

status: not-applicable

## Contract

- current Git現物とrepository authorityから、task authority・execution permission・execution resultを所有する既存境界を特定する
- 従来成功した`./install.sh`がapproval reviewerに拒否されたincidentの原因とpermission-relevant state差分を一次証拠から切り分ける
- task authority、execution permission、execution result、およびcommand-failed / execution-denied / permission-required / user-denied / user-input-required / task-blocked / task-terminalを暗黙統合しない
- 同一task・operation class・authority scope・execution boundaryで成立したpermissionを保持し、再要求には具体的なpermission-relevant state changeを必要とする
- session rotation後も保持可能なpermission stateをcanonical handoff/stateで維持し、実際に失われた場合だけ明示的なruntime stateとして扱う
- execution denialをuser denial、await-user、task terminalへ自動変換せず、既存permission/execution stateを解決してACTIVE taskを継続する
- 正当な新operation class・scope拡張・execution boundary変化では、差分とrequest scopeを明示した通常approvalを許可する
- 既存state machine / parent orchestrationへの最小変更とobservable state transitionの回帰試験で保証し、prompt-only対策にしない

## Must not

- 新しい巨大なpermission subsystemを作らない
- safety機構やapproval reviewerを迂回・無効化せず、全commandを無条件allowしない
- permissionを推測・捏造せず、external execution denialをuser denialまたはauthority欠如へ変換しない
- permission-relevant state changeの証拠なしに同一scopeのapprovalを再要求しない
- 今回事象と無関係な全面改修を行わない
- GLM worker/reviewerへGit remote write authorityを与えない

## Acceptance criteria

- permission state、task authority、execution resultの実装ownerと境界が特定されている
- permission convergenceが機械的に保証され、同一scopeの再要求はpermission-relevant state changeなしに拒否される
- execution denialだけではuser denial、await-user、ACTIVE停止、task terminalへ遷移しない
- session rotation時にpermission維持または実際のlossがcanonical stateとして識別可能である
- 正当な新permission requestはscopeと差分を明示して成立し、以後同一scopeで保持される
- Original instructionのRegression tests Case 1〜8をobservable state transitionとして直接再現するtestが追加され成功する
- relevant test、主要な既存test、lint/vet/build、repository固有quality gate、独立review、Sol semantic review、必要なruntime install/smokeが成功する
- prompt-only対策ではなくrepository implementationとして成立する

## Historical invariants

- parent-managed implementation metadataは親Codexだけが編集する
- GLM worker/reviewerにGit remote write authorityを与えない

## Dependencies

none

## Review findings

none

## Current boundary

`glm-parent-action install`を既存task admissionへbindする上記Sol decisionの範囲で実装・test・validationへ進む。relative `./install.sh` global allow、生成absolute rule、専用binary、新permission state machineは作らない。
