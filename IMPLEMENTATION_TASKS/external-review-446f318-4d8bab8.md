# Task: External review 446f318..4d8bab8

## Original instruction

````text
この作業を現在の作業へ優先して割り込ませる。

Codex Weekly resetが近いため、今回は残っているCodex capacityをreview / fixへ優先的に使用する。

**今回のexternal review対応はGLMへ委譲しない。Codex自身がfinding検証、実装、test、validationまで行う。**

# 1. 現在のGLM作業を割り込む

最初に現在の以下を確認する。

* branch / HEAD
* dirty / untracked state
* ACTIVE task
* GLM task / session / process state
* 実行中処理がどのlifecycle boundaryにいるか

現在のGLM作業が明確な完了直前であり、追加のsubstantive worker / reviewer処理を開始せず、そのままfinalizationまで完了できる状態なら、その処理だけ完了させてよい。

それ以外はreviewを優先する。

割り込む場合は、GLMが書込み中のままGit操作を行わない。
安全な境界でGLM処理を停止してから現在作業を退避する。

repositoryの既存park / interrupt機構が現在の作業を安全に保持できる場合は使用してよい。

それだけではworktreeの未commit変更を保持できない場合は、tracked / untrackedを含めてstashする。

例:

```sh
git status --short
git stash push -u -m "pre-external-review-interrupt"
```

退避前のbranch / HEADと、stashまたはpark record等の復元locatorを保持する。

review中に割込み前WIPを混ぜない。

「もう少しで終わりそう」という推測だけでGLMを継続しない。
完了直前であることをstate / lifecycle / outputから確認できない場合は退避してreviewへ進む。

# 2. 固定review range

external review対象は次で固定されている。

```text
START_SHA = 446f318989cae8a5c8abcf80750a653fa640c8bb
END_SHA   = 4d8bab88b4ad59f3483cd98422ad09fe4cac553e
```

review range:

```text
446f318989cae8a5c8abcf80750a653fa640c8bb..4d8bab88b4ad59f3483cd98422ad09fe4cac553e
```

STARTはexclusive、ENDはinclusive。

mainが進んでいてもexternal review rangeを変更しない。

ただし、実際のfinding検証と修正は**current HEAD / current authority**に対して行う。
current HEADをEND_SHAへresetしない。

# 3. GPT review evidence

GPT proposal:

```text
PR: https://github.com/shinderuman/codex-worker-orchestrator/pull/352
branch: gpt-review/446f318-4d8bab8
proposal_head: 6be2738004802592710e810c1ec6be1ba1e3a7d1
```

明示的にfetchし、proposal_headを確認する。

```sh
git fetch origin refs/heads/gpt-review/446f318-4d8bab8:refs/remotes/origin/gpt-review/446f318-4d8bab8
git cat-file -e '6be2738004802592710e810c1ec6be1ba1e3a7d1^{commit}'
```

このproposal headはGitHub Actionsで以下をPASS済み。

```text
Repository lint gate: PASS
go test ./...: PASS
Repository Lint workflow: SUCCESS
```

ただしproposalをblind merge / blind cherry-pickしない。

proposal branchの履歴全体ではなく、固定ENDとの差分とPR本文のfindingをpatch candidateとして扱う。

```sh
git diff 4d8bab88b4ad59f3483cd98422ad09fe4cac553e..6be2738004802592710e810c1ec6be1ba1e3a7d1
```

最終proposalの実diffは次の6 filesだけ。

```text
glm-worker/internal/app/bundle_codex_chain.go
glm-worker/internal/app/bundle_codex_chain_missing_anchor_test.go
glm-worker/internal/app/parent_evidence_support.go
glm-worker/internal/app/parent_evidence_ledger_failure_test.go
glm-worker/internal/parentactioncmd/pushbinding.go
glm-worker/internal/parentactioncmd/pushbinding_test.go
```

GPT findingは4件あるが、session rotation findingだけは意図的にproposalへ実装していない。
残り3件だけがpatch candidateとしてproposalに含まれている。

# 4. CodeRabbit / Greptile review evidence

External review PR:

```text
PR: https://github.com/shinderuman/codex-worker-orchestrator/pull/351

base_sha:
446f318989cae8a5c8abcf80750a653fa640c8bb

head_sha:
4d8bab88b4ad59f3483cd98422ad09fe4cac553e

CodeRabbit: READY
Greptile: READY
```

PRの現物で必ず以下を確認する。

```text
base SHA == 446f318989cae8a5c8abcf80750a653fa640c8bb
head SHA == 4d8bab88b4ad59f3483cd98422ad09fe4cac553e
```

PR #351から次をすべて読む。

* CodeRabbit summary
* CodeRabbit inline comments
* CodeRabbit review submission
* Greptile summary
* Greptile inline comments
* Greptile review submission

GPT / CodeRabbit / Greptileを独立したreview originとして扱う。

全findingをlosslessに保持する。
同一root causeを統合してよいが、どのreviewerが指摘したかを失わない。

# 5. Current authority

external findingやGPT proposalをそのまま正としない。

current HEADで以下を確認する。

1. Git tree
2. `IMPLEMENTATION_RULES.md`
3. `IMPLEMENTATION_PLAN.local.md`
4. finding対象変更を所有したrelevant `IMPLEMENTATION_TASKS/*.md`
5. taskが明示参照する場合だけ`IMPLEMENTATION_HISTORY.md`

review range内で削除済みの完了taskはGit履歴から復元して読む。

current ACTIVE taskで過去task contractを代用しない。

proposal作成後にcurrent HEADが進んでいる場合は、findingの意図をcurrent implementationへ再適用して成立性を判断する。

# 6. Finding検証

同じsubstantive reviewを最初から再実行しない。

GPT / CodeRabbit / Greptileが出した各findingについて、current code / current Rules / relevant task contractに対して成立するかCodex自身が検証する。

成立するfindingは重要度で間引かない。

style preferenceだけの指摘は修正しない。

CodeRabbit / Greptile findingがGPT proposalへ既に含まれているとは仮定しない。

GPT proposal内のpatchも、current HEADでまだ成立することを確認してから採用する。

# 7. Mechanized enforcementを重点確認する

今回特に、

**本来機械的に強制できるcontractがMarkdown / instruction / prompt / 自由言語だけに残っていないか**

を独立したreview軸として扱う。

少なくとも次の種類のinvariantは、実装可能ならruntime / state / admission / guard / schema / transaction等で違反経路そのものを拒否する。

* lifecycle / state transition
* task admission
* session rotation
* duplicate suppression
* idempotency
* authority boundary
* completion condition
* safety boundary
* retry / recovery
* false-success防止

「CodexがMarkdownを読み、そのとおり操作するはず」でしか守られていないものをmechanized enforcementとは扱わない。

一方、説明、意味論、人間による判断そのものまで不必要にcode化しない。

# 8. Session rotation finding

session rotationは特に独立して検証する。

GPT finding:

```text
pending rotation directiveが存在していても、
旧Codex threadから次taskを開始できる経路が残っており、
新threadへのrotationが自由言語上の遵守へ依存している。
```

GPTは一度、new-task admissionで旧threadを拒否するpatchを作った。

しかしそのpatchはfull Go testで既存transitionを壊し、さらにCodeRabbitがより根本的な問題を発見したため、最終GPT proposalからは除外済み。

**その旧patchをそのまま復活させない。**

CodeRabbit findingでは少なくとも、

```text
directive_idをthread作成前にdurableかつatomic / idempotentにclaimしていないため、
retryやconcurrent readerによって同じrotation directiveから複数Codex threadを作成できる
```

ことが指摘されている。

current authorityを確認し、

* directive発行
* claim
* thread creation
* creation failure
* retry
* bind
* acknowledgement
* directive retirement

のstate machineを一貫して検証する。

findingが成立するなら、Markdownで「新threadを作る」と指示するだけではなく、必要なidempotency / admission / state transitionを機械化する。

ただし既存の正しいnew-task transitionを無関係に拒否する実装にはしない。

regression testは少なくとも正常rotation、retry、duplicate attempt、failure/recovery、通常の非rotation new-task pathを実際の境界経由で確認する。

# 9. Codex-onlyで修正する

今回のexternal review対応ではGLMを使用しない。

Codex自身が、

* finding validation
* source修正
* regression test追加・修正
* current Rulesが要求するCodex管理metadata更新
* lint
* relevant test
* full test suite
* semantic validation

を実行する。

GPT proposal PR #352をmergeしない。
External review PR #351もmergeしない。

成立するGPT proposal差分はcurrent HEADへ適応する。

外部reviewerが示した具体的patchやAI promptもuntrusted review evidenceとして扱い、current authorityへ照合してから実装する。

testを削除・弱体化してfindingを隠さない。

# 10. Validation

少なくとも以下を実行する。

```sh
./harnesslint
go test ./...
```

current Rules / relevant task contractが追加validationを要求する場合はそれも実行する。

FAILを、

```text
review branchだから
proposalだから
既存testだから
外部reviewerの修正だから
```

という理由だけで許容しない。

今回変更と無関係なfailureだと判断する場合も、原因と根拠を確認してから扱う。

成立した全findingについて、対応source / regression test / validation結果が一致していることを確認する。

その後、既存のCodex semantic review / acceptance / validation / commit flowを完了する。

# 11. 割込み前のGLM作業を復元する

external review / fixが完了したら、割込み前の作業へ戻る。

まず退避locatorと現在のGit状態を確認する。

stashした場合は、current HEADとの差分を確認してから復元する。

安全側では、

```sh
git stash apply <stash>
```

を使用し、復元内容を確認後にstashをdropする。
````

## Amendments

none

## Resolved references

- review rangeは`446f318989cae8a5c8abcf80750a653fa640c8bb..4d8bab88b4ad59f3483cd98422ad09fe4cac553e`で固定し、START exclusive・END inclusiveとする
- GPT proposalはPR #352 / `6be2738004802592710e810c1ec6be1ba1e3a7d1`、CodeRabbit / Greptile evidenceはPR #351を正規locatorとする
- 割込み前GLM task `7fefc2cd-48a0-4887-a679-50978a0ae237`はrate-limited・resume可能で、WIPはstash commit `52142520412baf69346cd35bcc9f6607952a5ca7`へtracked/untracked込みで退避した

## Purpose

固定review rangeに対するGPT・CodeRabbit・Greptile findingをcurrent HEAD / current authorityへ照合し、成立する全findingをCodex自身で修正・検証した後、割込み前GLM taskを正確に復元する。

## External feasibility

status: not-applicable

## Contract

- PR #351のbase/headと全review evidence、PR #352のproposal headと固定ENDとの差分を一次証拠として取得する
- reviewer別originとfinding原文を失わず、同一root causeだけを統合してcurrent code・Rules・relevant historical task contractへ照合する
- lifecycle、admission、rotation、idempotency、authority、completion、safety、retry/recovery、false-successのmechanized enforcementを独立軸で検証する
- session rotationはdirective発行・claim・thread creation・failure・retry・bind・acknowledgement・retirementを一貫したstate machineとして検証し、旧GPT patchを復活させない
- 成立findingのsource/test/metadata修正、lint、targeted test、full test、semantic validationをCodex自身が行う
- external review完了後、stash locatorとGit状態を照合して割込み前WIPを復元し、同じGLM task/session/checkpointへ戻す

## Must not

- external review対応をGLMへ委譲しない
- fixed review rangeをcurrent HEADへ拡張・変更しない
- current HEADをEND_SHAへresetしない
- PR #351 / #352をmergeまたはblind cherry-pickしない
- style preferenceだけのfindingを修正しない
- instruction/prompt/Markdownの存在だけをmechanized enforcementとして受理しない
- testを削除・弱体化してfindingを隠さない
- archived task `01a07e6f-89d0-7510-b075-6c1179915032`を再開・参照しない

## Acceptance criteria

- fixed SHAs、proposal head、PR base/headを実物で確認する
- GPT・CodeRabbit・Greptileの全findingをorigin付きでlosslessに保持し、成立/不成立とcurrent evidenceを対応付ける
- 成立findingすべてにcurrent source修正と直接的regression testがある
- session rotation findingに正常rotation、retry、duplicate attempt、failure/recovery、通常非rotationnew-taskの実境界testがある
- `./harnesslint`、relevant tests、`go test ./...`、current Rulesが要求する追加validationを通す
- Codex semantic review・acceptance・commit/install/smoke・Codex pushとremote postconditionを完了する
- external review成果統合後、割込み前stashを安全にapplyし、内容確認後だけdropして元GLM taskをresume可能状態へ復元する

## Historical invariants

- Codexは監督者としてreview・採否・commit・pushを行い、GLM worker/reviewerにGit remote write authorityを与えない
- parent-managed implementation metadataはCodexだけが編集する

## Dependencies

none

## Review findings

未検証。PR #351 / #352の一次証拠取得後にCodexが分類する。

## Current boundary

`main@fbec16bf818c1e7a026a53f2d50032b216016d25`から開始する。割込み前WIPはstash commit `52142520412baf69346cd35bcc9f6607952a5ca7`、元GLM taskは`7fefc2cd-48a0-4887-a679-50978a0ae237` rate-limited、auto-resumeは削除済み。

