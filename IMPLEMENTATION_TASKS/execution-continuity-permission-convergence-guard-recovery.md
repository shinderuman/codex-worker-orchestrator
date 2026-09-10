# Task metadata

## Original instruction

````text
# Task: execution continuity / permission convergence / guard recovery の恒久改善

今回発生した長時間の停止・再許可要求・recovery loop・トークン浪費を、個別の応急処置ではなくrepository実装として恒久的に改善すること。

このtaskの目的は「permissionを求めなくすること」ではない。

また「エラーが起きたら停止すること」でもない。

最終目的は、

```text
必要なpermissionは正規に取得する
↓
成立したpermission stateを安定して維持する
↓
一時的なexecution/guard failureが発生しても収束するrecoveryを行う
↓
元のACTIVE taskへ必ず戻る
↓
同じrecoveryを反復してトークンを浪費しない
↓
開発作業を継続する
```

ことである。

---

# Incident 1: permission negotiationによる停止

同じACTIVE taskを継続しているにもかかわらず、それまで実行できていた操作が途中からapproval対象となり、Codexが、

```text
execution denied
→ user permission不足と解釈
→ conversational permission request
→ await-user
→ 作業停止
```

へ遷移した。

ユーザーは新しいpermissionを拒否しているわけではない。

本当に新しいpermissionが必要なら正規のapprovalを行ってよい。

問題は、

```text
許可
→ 少し進む
→ また許可不足
→ 停止
→ 再許可
→ また停止
```

というpermission stateの非収束である。

---

# Incident 2: Git authority guardによるrecovery loop

current implementationを確認すると、

`glm-worker/internal/runner/git_authority_guard.go`

のauthority snapshotは、

```text
git for-each-ref
```

の全ref出力をdigest化している。

また、

`glm-worker/internal/workflow/guard_recovery.go`

のguard recoveryは、resume前にcurrent full-ref digestがpre-call digestと完全一致することを要求している。

一方、Codex Desktop自身が、

```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

等の内部refをtool実行・checkpoint・session処理に伴って変更する。

このため、

```text
guard snapshot
→ Desktop内部ref変更
→ guard-recoverable
→ refを復元
→ Desktopが再変更
→ full-ref digest不一致
→ resume拒否
→ 再度復元
```

という非収束recovery loopが発生した。

実際に同じresume/recoveryを複数回繰り返し、開発作業へ戻らないまま大量のCodex usageを消費した。

---

# Required architecture investigation

まずcurrent Git現物とrepository authorityを読み、以下の責務境界を確認すること。

```text
gitAuthorityGuard
guard recovery
resume checkpoint
parent action
permission / approval handling
session rotation
ACTIVE task lifecycle
```

既存設計を確認せず、新しい独立subsystemを追加してはならない。

prompt-onlyの改善で完了としてはならない。

---

# 1. Git authority guardの監視対象を正しく定義する

現在の「全Git refを一律authorityとしてdigest化する」という設計が本当に必要かを監査すること。

guardの目的は、worker/modelによるrepository authorityの不正変更を検知することであり、

Codex Desktop自身が管理するvolatile metadataを固定することではない。

少なくとも、

```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

について、

* repository authorityなのか
* Desktop runtime metadataなのか
* guard対象に含める意味があるのか

をcurrent behaviorとtestから判断すること。

volatile Desktop-managed refであると確認できたものはauthority digestから除外する。

ただし、

```text
refs/codex/*
```

全体を理由なく除外してはならない。

監視対象は意味に基づいて限定すること。

HEAD、symbolic HEAD、index、local config、production/repository authority refなど、実際に保護する意味があるstateは維持すること。

---

# 2. recoveryをfull-ref digest完全一致へ依存させない

guard failureで具体的なref change evidenceが取得できている場合、

```text
repository全refが過去のsnapshotと完全一致するまでresume不可
```

というrecovery設計が必要かを見直すこと。

Desktop/runtimeが合法的に変更するvolatile stateによってresume不能になってはならない。

recovery validationは、

```text
「保護対象のauthorityが復元された」
```

ことを確認するためのものにする。

```text
「repository内の全可変metadataが過去と完全一致した」
```

ことを要求してはならない。

---

# 3. recoveryは必ず収束させる

同一failure fingerprintについて同じrepair strategyを何度も繰り返してはならない。

概念上、

```text
failure fingerprint
+ recovery strategy
```

を追跡する。

同一条件で一度失敗したrecoveryを、追加evidenceなしに再実行してはならない。

特に以下は禁止する。

```text
同じrefを何度も復元
同じdigest一致確認を何度も実行
同じresumeを何度も実行
同じhandoff/recovery evidenceを何度も探索
過去のvolatile ref集合を延々と逆算
```

同一strategyが成立しないと判明した時点で、そのstrategyをterminal failureとして扱い、別の正規なrecovery pathへ移ること。

---

# 4. productive progressを保証する

recovery処理自体を成果として扱わない。

以下のような処理だけが続いている状態はproductive progressではない。

```text
status再取得
handoff再取得
同一ref調査
digest再計算
同一resume再試行
同じauthority再読
permission説明
```

recoveryの成功条件は、

```text
recovery state resolved
```

だけではなく、

```text
original ACTIVE task resumed
```

まで含めること。

可能ならparent orchestrationで、

```text
RECOVERY
→ RESUME
→ ORIGINAL_WORK_RUNNING
```

まで確認する。

---

# 5. ACTIVE taskを保持する

execution failure、guard failure、approval failureを理由に、モデル判断だけでACTIVE taskを停止・cancel・置換してはならない。

次を別stateとして扱うこと。

```text
operation failed
execution denied
guard recoverable
permission required
user input required
task blocked
task terminal
```

これらを同義にしない。

特に、

```text
execution denied != task stop
guard failure != task stop
recovery failure != user input required
```

を保証すること。

一時的なinline repairが必要でも、

```text
ACTIVE task
→ blocker inline repair
→ same ACTIVE task resume
```

とする。

勝手に、

```text
ACTIVE task
→ repair task作成
→ original task停止
```

へ遷移してはならない。

---

# 6. permission convergence

task authorityとexecution permissionを分離すること。

```text
task authority
execution permission
execution result
```

は別stateである。

execution layerからの拒否を、

```text
user denied
```

へ変換してはならない。

ユーザーが、

```text
今までできていた作業をそのまま続けろ
```

と指示した場合、

新しいpermission grantでもpermission denialでもない。

既存task authorityのまま作業を継続するrun-control指示として扱う。

---

# 7. legitimate permission requestは残す

permission requestそのものを禁止してはならない。

本当に、

```text
新しいoperation class
新しいauthority scope
新しいexecution boundary
実際のsandbox/policy変更
```

が発生した場合は正規にpermissionを要求してよい。

ただし再要求には、

```text
以前のpermission成立時点から何が変わったのか
```

を機械的に説明できるstate changeが必要。

同一task・同一scope・同一execution boundaryで、理由なくpermission negotiationへ戻ってはならない。

---

# 8. session rotation

session rotationだけを理由にpermission、authority、recovery stateをモデル推測で初期化してはならない。

rotationを跨いで保持すべきstateはcanonical state/handoffへ保存すること。

host側のsession-local permissionが本当に失われる場合は、

```text
rotationによるexecution permission loss
```

として明示的に扱う。

単に「新しいsessionなので再許可が必要そう」と判断してはならない。

---

# Regression tests

今回のincidentを直接再現するtestを追加すること。

最低限、以下を検証する。

## A. volatile Codex ref

guard実行中にDesktop-managed volatile refが、

```text
追加
削除
OID変更
```

されても、repository authority mutationとして誤検知しない。

対象namespaceは実装調査で正当化すること。

## B. real authority ref

本当にguard対象であるrefが変更された場合は従来どおり検出する。

## C. HEAD / index / config

既存の保護を維持する。

## D. guard recovery

volatile runtime stateの変更だけで、

```text
pre-call full-ref digestへ完全復元できない
→ resume永久拒否
```

にならない。

## E. repeated recovery

同一failure・同一strategyを追加evidenceなしで反復できない。

## F. recovery continuation

recovery後、元のACTIVE taskがrunnableとなり、original workflowへ復帰する。

## G. permission convergence

一度成立した同一scopeのpermissionを、permission-relevant state changeなしに再要求しない。

## H. legitimate new permission

本当に新しいexecution boundaryが必要な場合は正常にpermission requestできる。

## I. user continuation instruction

```text
今までできていた作業をそのまま続けろ
```

を、

```text
permission grant
permission denial
new task
stop request
```

のいずれにも変換しない。

---

# Token waste prevention

同一failureについて調査だけを続けない。

既に実装修正に十分な原因が特定された場合、追加探索よりproduction fixを優先する。

「さらに調べれば過去状態を完全復元できるかもしれない」という理由だけで、非収束探索を続けてはならない。

recovery処理には、

```text
同一failure fingerprint
同一strategy
productive progress
```

を使ったboundedなloop controlを設けること。

単純な時間制限や固定回数だけで雑に停止するのではなく、同じ失敗を繰り返していることを検知すること。

---

# Non-goals

以下は目的ではない。

* safety機構の無効化
* approval reviewerの迂回
* 全commandの無条件allow
* permission requestの全面禁止
* Git authority guardそのものの削除
* guard failureを無視すること
* recovery不能時に黙って作業停止すること
* timeoutだけで強制終了すること
* promptに「止まるな」と追記するだけの対応
* 今回事象と無関係な大規模全面改修

---

# Completion criteria

以下をすべて満たした場合のみ完了。

1. current implementation上のguard/recovery/permission責務境界を確認済み
2. Desktop/runtime volatile stateとrepository authorityを実装上区別済み
3. volatile ref変更だけでfalse guard failureにならない
4. recoveryが過去のfull-ref snapshot完全復元へ不必要に依存しない
5. 同一recovery strategyを非収束反復できない
6. recovery完了後に元のACTIVE taskへ実際に復帰する
7. execution failureだけでACTIVE taskを停止しない
8. permission stateが同一scopeで理由なく再交渉されない
9. 本当に新しいpermissionが必要な場合は正規に要求できる
10. session rotation後のstate継続が明示的
11. 今回のincidentを再現するregression testが存在する
12. 主要test / validationがPASS
13. prompt-onlyではなくproduction implementationとして成立している

最重要条件:

```text
「許可を聞かなくなった代わりに作業が停止する」
という修正はFAIL。

「recovery loopを止めた代わりに作業が停止する」
という修正もFAIL。

改善後は、問題発生時にも最終的に元のACTIVE taskへ戻り、
開発作業が前進しなければならない。
```
````

## Amendments

### 2026-09-10 update

````text
# Task: execution continuity / permission convergence / self-blocking guard recovery の恒久改善

今回発生した長時間の作業停止、permission再要求、guard recovery loop、Codex token浪費を、個別の応急処置ではなくrepository実装として恒久的に改善すること。

このTaskの最終目的は次の3つを同時に満たすことである。

```text
1. 作業を停止させない
2. recoveryのためにCodex parent tokenを浪費しない
3. safety / authority guardを単純に無効化しない
```

次のような修正はFAILとする。

```text
permissionを聞かなくなったが作業停止する
recovery loopを止めたが作業停止する
作業は続くがCodex parentが何ターンもrecoveryを考え続ける
guardを無効化して無理やり通す
```

---

# Incident

## 1. permission stateの非収束

同じACTIVE taskを継続しているにもかかわらず、それまで通常実行できていたoperationが途中からapproval対象となり、

```text
execution denied
→ user permission不足と解釈
→ conversational approval request
→ await-user
→ ACTIVE task停止
```

へ遷移した。

ユーザーは新しいpermissionを拒否しているわけではない。

本当に新しいpermissionが必要なら、正規のapproval procedureを使用してよい。

問題は、

```text
permission成立
→ 作業継続
→ 同じtask中に理由なく再度permission不足
→ 作業停止
→ 再permission negotiation
```

が繰り返されることである。

---

## 2. Git authority guard false positive

current implementationでは `gitAuthorityGuard` が `git for-each-ref` の全refをsnapshot/digest対象としている。

Codex Desktop自身が管理するvolatile refもその対象となるため、

```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

等のruntime内部refがDesktopによって追加・削除・更新されるだけでguard failureになり得る。

これはHEAD / index / worktreeやrepository authorityの変更とは別である。

---

## 3. guard recoveryの自己閉塞

より重大なのは、guard failure後の正規resume path自身が同じguard stateに依存していることである。

現在の構造では概念的に、

```text
GLM worker
→ post-call guard failure
→ guard-recoverable
→ resume
→ pre-call ref digest復元要求
→ Desktop内部refが既に変化
→ resume拒否
```

となる。

その結果、

```text
guardを修正するためにGLMを呼びたい
↓
GLMを呼ぶ正規入口がresume
↓
resume自身が壊れたguard条件で拒否
↓
GLMへ修正を依頼できない
```

というself-blocking状態が発生する。

今回、この状態から過去ref/digest復元を長時間反復し、実開発を進めないまま大量のCodex tokenを消費した。

---

# Required architecture

通常の実行経路と、通常経路自身が壊れた場合のrepair経路を分離すること。

概念上、

```text
normal path:

parent orchestration
→ normal guard
→ GLM
→ review
→ continuation
```

に対して、

```text
self-blocking recovery:

normal path self-blocked
→ mechanical detection
→ bounded repair path
→ repair GLM
→ validation
→ original ACTIVE taskへ復帰
```

を持つこと。

repair pathは単純な `--no-guard` や全面的なguard bypassにしてはならない。

---

# 1. Desktop volatile refsをrepository authorityから分離する

current `gitAuthorityGuard` の監視対象を監査すること。

少なくとも、

```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

について、Codex Desktop runtimeが所有するvolatile metadataであることを確認したうえで、repository authority digest/ref-change判定から除外する。

ただし、

```text
refs/codex/*
```

全体を無条件除外してはならない。

意味のあるrepository authority refまで失わないよう、対象namespaceを限定すること。

以下の保護は維持する。

```text
HEAD
symbolic HEAD
index
local Git config
production/repository authority refs
その他guard本来の保護対象
```

---

# 2. recoveryをfull-ref snapshot完全復元へ依存させない

guard recoveryの目的は、

```text
保護対象repository authorityが安全な状態である
```

ことの確認である。

```text
repository内の全volatile metadataが過去のある瞬間と完全一致する
```

ことを目的にしてはならない。

具体的なguard evidenceがある場合、保護対象authorityの差分だけを検証する。

Desktop/runtime-owned volatile stateが進行したことだけでresume不能にならないようにする。

---

# 3. self-blocking stateを機械的に検出する

normal recovery path自身が、修復対象のguardによって利用不能になった状態を明示的に表現すること。

例えば概念上、

```text
guard-recoverable
normal resume attempted
same guard/recovery condition prevents resume
```

を、

```text
normal-recovery-self-blocked
```

相当のmachine stateとして判定可能にする。

名称・型は既存設計に合わせること。

自然言語でCodexが「どうもself-blockingらしい」と判断する設計にしない。

---

# 4. bounded break-glass repair pathを実装する

normal recoveryがself-blockingした場合に限って使用できるrepair pathを実装すること。

これは通常guardの全面無効化ではない。

repair pathには通常経路とは別の狭いmachine-enforced authority boundaryを持たせる。

最低限、次を強制すること。

```text
- original ACTIVE task identityを保持する
- original task checkpoint/resultを保持する
- NEXT taskへ進まない
- Plan上のACTIVEを切り替えない
- production branch/refを任意変更しない
- repair対象外のsource変更を許さない
- repair対象を明示的・boundedにする
- repairに対応するtestを必須にする
- validation失敗を隠さない
- repair完了後はoriginal ACTIVE taskへ復帰する
```

break-glass pathを、ユーザーが任意commandをguard外で実行できる一般的な裏口にしてはならない。

---

# 5. repairをGLMへ依頼できる経路を確保する

今回のようにnormal `start/resume` がguard自身によって塞がれた場合でも、repair対象をGLMへ渡せること。

概念上、

```text
glm-parent-action repair
```

のような専用actionでもよいが、名称・CLI設計はcurrent architectureに合わせて判断すること。

重要なのは、

```text
normal resumeが動かない
→ Codex parentがsourceを直接直すしかない
```

という構造をなくすことである。

repair GLM invocationはnormal broken checkpoint guardをそのまま通して自己閉塞してはならない。

代わりにrepair専用の限定guard/authorityを使用すること。

---

# 6. repair後はoriginal ACTIVE taskへ自動復帰する

repair完了をTask completionとして扱ってはならない。

必須state transitionは、

```text
original ACTIVE task
→ self-blocking failure
→ repair
→ repair validation
→ original checkpoint/state再評価
→ original ACTIVE task resume
→ original development work
```

である。

以下は禁止する。

```text
repair完了
→ ユーザーへ報告
→ stop
```

```text
repair完了
→ new task待ち
```

```text
repair完了
→ NEXT task
```

```text
repair完了
→ original implementation diff破棄
```

repairの成功条件には、

```text
ORIGINAL_WORK_RESUMED
```

まで含めること。

---

# 7. recovery loopをCodex parentへ戻さない

既知のcontrol-plane recovery処理をCodex parent modelのreasoning loopとして実装してはならない。

次の処理は可能な限りGo側のmachine logicで行う。

```text
guard state classification
failure fingerprint
volatile ref classification
checkpoint validation
normal recovery可否
self-blocking判定
同一recovery反復防止
repair action選択
repair後のresume
session state引継ぎ
canonical handoff generation
```

次のようなloopは禁止する。

```text
failure
→ Codex parent
→ status確認
→ command
→ Codex parent
→ handoff確認
→ command
→ Codex parent
→ ref探索
→ ...
```

既知state machineで処理できる場合、parent model invocationを増やしてはならない。

---

# 8. same failure / same recoveryを反復しない

少なくとも概念上、

```text
failure fingerprint
recovery strategy
relevant state
```

を追跡する。

次が同一なら、

```text
same failure
same recovery
no new relevant evidence
```

同じrecovery actionを再実行してはならない。

特に今回発生した、

```text
ref復元
→ resume
→ failure
→ ref復元
→ resume
→ failure
```

をmachine-levelで不可能にする。

固定回数timeoutだけで雑に止めるのではなく、同一failureへの同一strategy反復を検知すること。

---

# 9. token consumptionをfailure handlingの設計指標にする

このTaskでは、単に「最終的に復旧できた」だけでは不十分。

既知のcontrol-plane failure処理で不要なCodex parent invocationを発生させないこと。

可能な既存telemetry/call accountingを用いて、

```text
known mechanical recovery
→ parent model call count does not increase
```

をtest可能にする。

新しい複雑なtoken accounting subsystemは不要。

既存call accountingから必要十分な検証を行う。

---

# 10. permission convergence

task authority、execution permission、execution resultを分離する。

```text
task authority
execution permission
execution result
```

は別stateである。

execution reviewer / sandbox / host policyによる拒否を、

```text
user denied
```

へ暗黙変換しない。

同一ACTIVE task・同一scope・同一execution boundaryで、一度成立したpermissionを具体的なstate changeなしに再要求しない。

本当に新しいpermissionが必要なら正規に要求してよい。

その場合は、

```text
何が以前から変わったか
なぜ既存permissionでは不足するか
どのscopeが新しく必要か
```

をmachine stateから説明可能にする。

---

# 11. permission問題でも作業継続性を失わない

permission requestを単純禁止して終わってはならない。

以下はFAIL。

```text
permissionが必要
→ 聞くことは禁止されている
→ BLOCKED
→ stop
```

目的は、

```text
必要なpermissionは正規に処理
→ permission stateを収束
→ original task継続
```

である。

同様に、

```text
permissionを再要求しなくなった
→ 黙って停止
```

もFAIL。

---

# 12. session rotation

session rotationによってLLM conversationが変わっただけで、

```text
task authority
permission state
checkpoint state
recovery state
```

をモデル推測で初期化してはならない。

保持可能なstateはcanonical persisted stateとして引き継ぐ。

host/session-local permissionが本当に消える仕様の場合だけ、それを明示的なruntime transitionとして扱う。

---

# Required regression tests

current test architectureに従い、以下を実際のobservable behaviorとして検証すること。

## A. Desktop volatile ref

guard call中に、

```text
refs/codex/turn-diffs/*
refs/codex/snapshots/*
```

の追加・削除・OID変更が発生する。

期待:

```text
repository authority mutationとして誤検知しない
```

---

## B. real protected ref mutation

実際のguard対象refを変更する。

期待:

```text
従来どおりguard failure
```

---

## C. HEAD / index / config protection

既存guard protectionを維持する。

---

## D. normal guard recovery

通常復元可能なguard failureは既存normal recoveryで収束する。

---

## E. self-blocking normal recovery

normal recovery自身がguard条件によって利用不能になる状態を作る。

期待:

```text
同じresumeを反復しない
self-blockingをmachine detection
bounded repair pathへ移行
```

---

## F. repair scope enforcement

repair pathが許可されたrepair対象のみ変更できる。

対象外変更は拒否される。

---

## G. repair GLM invocation

normal GLM resume pathが自己閉塞していても、限定repair pathからrepair workerを起動できる。

---

## H. repair → original resume

repair完了後、

```text
original ACTIVE task
original implementation state
original checkpoint/result
```

を保持して元workflowへ復帰する。

ユーザー入力を要求しない。

---

## I. repeated recovery prevention

同一failure fingerprint、同一strategy、追加evidenceなしで同じrecoveryを再実行できない。

---

## J. parent token protection

既知mechanical recovery pathでは、recovery判断だけのためにCodex parent model invocation countが増えない。

---

## K. permission convergence

成立済み同一scope permissionを理由なく再要求しない。

---

## L. legitimate new permission

実際に新しいexecution boundary/scopeが必要な場合は、正規のpermission requestが可能。

---

## M. continuation

次のようなユーザーrun-control、

```text
今までできていた作業をそのまま続けろ
作業停止せず開発を続けろ
```

を、

```text
new permission grant
permission denial
new task request
stop request
```

へ変換しない。

existing ACTIVE taskを継続する。

---

# Token-waste regression

今回のincident相当のscenarioを通し、

```text
volatile Desktop ref change
→ guard/recovery problem
→ repair
→ original task resume
```

までの間に、

```text
同一resume反復
過去ref総当たり
SQLite/rolloutから削除ref探索
過去full-ref digest復元探索
同じhandoff/status取得の反復
permission negotiation反復
```

が発生しないことを確認する。

---

# Non-goals

以下は行わない。

```text
- Git authority guard全体の削除
- refs/codex/* 全体の無条件除外
- safety reviewerの迂回
- arbitrary command用のunguarded execution path
- permission request全面禁止
- 全commandへの恒久allow
- recovery不能時に黙って停止
- timeoutだけによる問題隠蔽
- promptに「止まるな」と書くだけの対策
- unrelated architecture rewrite
```

---

# Implementation policy

current Git現物とrepository authorityを最初に確認すること。

既存の、

```text
runner
workflow
state
parent action
handoff
rotation
guard recovery
permission/run-control
```

の責務境界を理解してから変更すること。

既存設計に自然に収まる最小構成を優先する。

不要なinterface、抽象化、巨大な新規subsystemを作らない。

実装変更には対応するGo testを必ず追加・更新する。

---

# Completion criteria

以下をすべて満たした場合のみTask完了。

1. Desktop volatile refとrepository authority refが実装上分離されている
2. volatile ref変化だけでfalse guard failureにならない
3. recoveryが不要なfull-ref snapshot完全一致へ依存しない
4. normal recoveryのself-blockingをmachine detectionできる
5. self-blocking時にbounded repair pathが利用できる
6. repair pathは通常guardの全面bypassではない
7. normal GLM pathが塞がれていてもrepair GLMを利用できる
8. repair後にoriginal ACTIVE taskへ自動復帰する
9. original task diff/checkpoint/resultを不必要に破棄しない
10. 同一failure / 同一recoveryを反復できない
11. known recoveryで不要なCodex parent model callを発生させない
12. permission stateが理由なく再交渉されない
13. legitimate new permission requestは維持される
14. permissionを求めなくした結果、作業停止する構造になっていない
15. recovery loopを止めた結果、作業停止する構造になっていない
16. session rotation後もcanonical stateから継続できる
17. incident相当のregression testが存在する
    18.主要なGo tests / race / repository quality gatesがPASSする
18. prompt-onlyではなくproduction implementationとして成立している

最重要不変条件:

```text
normal path failure
    != task stop

guard recovery failure
    != endless Codex reasoning

permission problem
    != repeated conversational negotiation

self-blocking guard
    → bounded mechanical repair
    → original ACTIVE task resume

known control-plane recovery
    → minimal parent token consumption

repair success
    → original development work actually resumes
```

このTaskの成果は「recovery機能を追加したこと」ではない。

**Codex/GLM control-plane自身に障害が起きても、作業を停止せず、不要なCodex tokenを消費せず、安全境界を維持したまま元の開発作業へ復帰できること**を成果とする。
````

## Resolved references

- 2026-09-10の現在ACTIVE taskで、`refs/codex/turn-diffs/*`と`refs/codex/snapshots/*`をGit authority digest・ref-change判定から除外するinline repairと回帰testを先行実装した。completed current diffを本Task開始時の一次根拠として再検証し、重複実装しない。

## Purpose

permission state、guard recovery、session rotation、ACTIVE task lifecycleを収束可能なproduction制御として統合し、同一失敗の反復や会話上の再許可要求で元Taskの実作業が停止する再発を防ぐ。

## External feasibility

status: not-applicable

## Contract

- Original instructionのIncident 1・2、責務境界1〜8、Regression tests A〜I、Token waste prevention、Completion criteriaをcurrent implementationへ照合し、prompt-onlyでないproduction実装とtestで成立させる
- current ACTIVEで先行したvolatile ref除外・legacy guard recovery変更を再利用し、正当性を再確認した上で重複layerを追加しない
- 同一failure fingerprintとrecovery strategyの再実行を追加evidenceなしに許さず、別の正規recovery pathまたは明示的terminal stateへ収束させる
- recovery成功を元ACTIVE taskのrunnable化とoriginal workflow復帰まで追跡する
- task authority、execution permission、execution resultを別stateとして扱い、execution denialをuser denialへ変換しない
- legitimateな新permission boundaryは正規要求として残し、同一scopeの再交渉にはpermission-relevant state changeを必須にする
- session rotationを跨ぐauthority・permission・recovery stateをcanonical state/handoffで保持する
- normal recovery自身のself-blockingをmachine stateとして検出し、通常guardの全面bypassではないbounded repair GLM pathへ遷移する
- repair scope、original task identity/checkpoint/result、validation、original workflow復帰を単一のmachine lifecycleとして強制する
- known mechanical recoveryはparent reasoning loopへ戻さず、既存call accountingで不要なparent invocation増加がないことを検証する

## Must not

- safety機構、approval reviewer、Git authority guardを無効化・迂回しない
- `refs/codex/*`全体をvolatile扱いしない
- timeoutや固定回数だけでrecovery convergenceを代替しない
- ACTIVE taskを一時failureだけでcancel・置換しない
- promptへ停止禁止を追記するだけで完了しない
- current incidentと無関係な全面改修へ拡張しない
- arbitrary command用のunguarded repair pathを追加しない

## Acceptance criteria

- Original instructionのRegression tests A〜Iをproduction behaviorに対して実行しPASSする
- Completion criteria 1〜13をcurrent snapshot上で満たす
- 同一failure・同一strategyの反復拒否と、元ACTIVE taskへの復帰をmachine-readable stateで確認できる
- permission-relevant state changeの有無により再要求と継続を機械的に区別できる
- self-blocking detection、bounded repair invocation、repair scope enforcement、original task自動resume、parent token protectionをobservable behaviorで検証する
- independent reviewer、Sol semantic review、current snapshot validation、必要なinstall/smokeを完了する

## Historical invariants

- GLM worker/reviewerにGit remote write authorityを付与しない
- parent-managed implementation metadataは親Codexだけが編集する

## Dependencies

none

## Review findings

none

## Current boundary

未着手。現在ACTIVEのinline guard repairと元Task復帰を完了後、直近NEXT群のうち本incidentの再発性・productive progress阻害を最優先してACTIVE化する。
