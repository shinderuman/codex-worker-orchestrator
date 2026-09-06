# Task: External review follow-up for 43e1da9..446f318

## Original instruction

````text
EXTERNAL_REVIEW_INTAKE

range: 43e1da969a4118122e65c09863794028a0ddb139..446f318989cae8a5c8abcf80750a653fa640c8bb

gpt_review:\
status: PROPOSAL\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/349\
branch: gpt-review/43e1da9-446f318\
proposal_head: b2b3951181b9ec3afcdcfdb11b8d84cdae52f05c\
scope: full range

greptile_review:\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/348\
status: READY\
scope: full range\
base_sha: 43e1da969a4118122e65c09863794028a0ddb139\
head_sha: 446f318989cae8a5c8abcf80750a653fa640c8bb

coderabbit_review:\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/350\
status: PENDING\
scope: a265947b768aa2e57cfaa263019f0377df0d539b only\
base_sha: 93a1ccf9d86ea470bf0d84dbc925b43c489738ae\
head_sha: a265947b768aa2e57cfaa263019f0377df0d539b

CodeRabbit exception:

- full-range PR #348は123 files制限でCodeRabbit reviewに失敗した
- 今回に限りCodeRabbitはfull rangeを要求しない
- #348上のCodeRabbit結果はtransport sourceとして使わない
- CodeRabbit evidenceはPR #350のa265947単独reviewだけを使用する
- #350が完了するまでCodeRabbitをfindingなし扱いしない

Codex:

- current repository authorityを再確認する
- GPT branchをfetchしproposal_headを確認する
- PR #349からGPT findingとproposal diffを取得する
- PR #348からGreptileの全findingを取得する
- PR #350からCodeRabbitの全findingを取得する
- originをGPT / Greptile / CodeRabbitで保持したまま全findingをlosslessにGLMへ渡す
- 同じsubstantive reviewをGLM前に最初から再実行しない

GPT proposal fetchまたはproposal_head確認に失敗した場合はtransport failureとして停止する。

PR #348のbase/headがfull rangeと一致しない場合はtransport failureとして停止する。

PR #350のbase/headが\
93a1ccf9d86ea470bf0d84dbc925b43c489738ae..\
a265947b768aa2e57cfaa263019f0377df0d539b\
と一致しない場合はtransport failureとして停止する。

PR #350のCodeRabbit reviewがまだPENDINGまたはFAILEDならGLMへ進まない。

GLM:

- current HEAD / current Rules / relevant task contractに対してGPT / Greptile / CodeRabbitの全findingを検証する
- GPT proposalを検証し、staleなら意図を保持してcurrent HEADへ適応する
- 成立する問題だけを修正し必要なtestを実行する
- 同一root causeのfindingは統合してよいがoriginを失わない

GPT proposal PRをblind mergeしない。\
External review PR #348 / #350もmergeしない。

GLM処理後は既存のCodex semantic review / acceptance / validation / commit flowへ戻る。
````

## Amendments

none

## Resolved references

- GPT proposal branch `gpt-review/43e1da9-446f318`のremote headとfetch済みcommitはいずれも`b2b3951181b9ec3afcdcfdb11b8d84cdae52f05c`。`--no-write-fetch-head --no-tags`で取得し、fetch前後のrepository refsは一致した
- GPT proposal diff locatorは`git diff --binary --full-index 446f318989cae8a5c8abcf80750a653fa640c8bb b2b3951181b9ec3afcdcfdb11b8d84cdae52f05c`。6 files、165 insertions、5 deletions、取得時9650 bytes、FNV-1a 32は`fb25838a`
- 2026-09-07のGitHub API確認でPR 350のbase/headは`93a1ccf9d86ea470bf0d84dbc925b43c489738ae..a265947b768aa2e57cfaa263019f0377df0d539b`と一致した
- CodeRabbit review `https://github.com/shinderuman/codex-worker-orchestrator/pull/350#pullrequestreview-5126852868`はcommit `a265947b768aa2e57cfaa263019f0377df0d539b`に対し2026-09-06T22:49:40ZにCOMMENTEDで完了し、actionable inline finding 13件を報告した
- CodeRabbit完了通知は`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#issuecomment-5562689841`の`Review finished.`。PR 348上のCodeRabbit結果は使用しない
- PR 349のbase/headは`446f318989cae8a5c8abcf80750a653fa640c8bb..b2b3951181b9ec3afcdcfdb11b8d84cdae52f05c`、head branchは`gpt-review/43e1da9-446f318`で指定値と一致した
- PR 348のbase/headは`43e1da969a4118122e65c09863794028a0ddb139..446f318989cae8a5c8abcf80750a653fa640c8bb`、Greptile review commitも`446f318989cae8a5c8abcf80750a653fa640c8bb`で指定full rangeと一致した
- GPT finding 1、PR 349本文、`https://github.com/shinderuman/codex-worker-orchestrator/pull/349`:
  - `Review-gap category false-known without a previous round.`
  - `reviewGapFillCategories`はprevious round欠損時にcurrent round全体をfix deltaとして扱う一方、semanticityはunknownのままであり、fix categoryとsummary countを誤帰属し得る。proposalはcategoryを`previous-round-missing`付きunknownに保ち回帰testを追加する
- GPT finding 2、同PR本文:
  - `Parent evidence byte/token proxy undercounts projected bodies.`
  - Authorityはdigest lengthへfallbackし、validationsとtelemetryはrecord countをbytesとしていたため、model-visible evidence bytes/token proxy契約を破る。proposalはauthorityのemitted contentとvalidation/telemetryのJSON payload byte lengthを測定し回帰testを追加する
- GPT finding 3、同PR本文:
  - `Unpark can resume before an interrupt commit is integrated when the original HEAD is unchanged.`
  - provenance checkがadvanced interrupt branchを検査する前に`head-unchanged`を返すため未統合interrupt resultでもresumeし得る。proposalはinterrupt branchを先に検査し未統合を拒否する回帰testを追加する
- GPT proposalは上記3 findingを6 filesの変更へ限定し、test未実行のためPASSを主張していない。current checkoutでcompile/unit validationが必要
- Greptile finding 1、P2、`glm-worker/internal/workflow/park.go:222`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/348#discussion_r3944929129`:
  - `Unpark leaks temporary resources.`
  - successful unpark後にtask statusを復元するが、parking時に作成したinterrupt worktree、branch、sibling state、park record、copied dirty-file contentsを削除しないため、park cycleごとにrepository refsとdisk artifactsが残る
- Greptile summary、`https://github.com/shinderuman/codex-worker-orchestrator/pull/348#issuecomment-5561387139`:
  - Confidence Score 4/5。correctness上概ねsafeだが、successful unpark後にtemporary Git worktreeとpark stateをleakするnon-blocking cleanup defectがある。Files Needing Attentionは`glm-worker/internal/workflow/park.go`と`glm-worker/internal/state/park.go`
- CodeRabbit finding 1、Minor / Maintainability、`codex/instructions/glm-repo-search.md:14`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509312`:
  - `Correct the state-change contract for --repo-search.`
  - 現記述はstate changeなしとするが、enabled searchはparent-evidence telemetryとledger entryを保存する。repository/lifecycle stateは変更しない一方、parent-evidence telemetry/ledger stateは書くと明示する
- CodeRabbit finding 2、Major / Data Integrity、`glm-worker/internal/app/parent_evidence_support.go:123`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509314`:
  - `Make the parent-evidence claim atomic.`
  - `ModeStatus`、`ModeHandoff`、`ModeRepoSearch`はrepo lock取得前に`executeStateless`を通り、`decideParentRead`のledger loadと後続saveが同期されない。並行processが同じdecision leaseで同surface/digestを二重投影できるため、atomic claimとrender/output failure時のreleaseが必要
- CodeRabbit finding 3、Minor / Data Integrity、`glm-worker/internal/app/parent_evidence.go:547,574`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509315`:
  - `Set part.Bytes from rendered byte length, not from record counts.`
  - validationは`len(records)`、telemetryは`body.Records`をbytesへ設定し、`recordPart`のTokenProxyとsummary aggregateを過少計上する。rendered payloadのbyte lengthを使う
- CodeRabbit finding 4、Major / Data Integrity、`glm-worker/internal/authoritybootstrapcmd/run.go:67-69`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509317`:
  - `Normalize knownContentSHA to lowercase in BuildFromRoot.`
  - uppercase digestはvalidationを通るがlowercase `ContentSHA256`と一致せず、evidence manifestのunchanged-body suppressionに反してfull authority bodyを返す。validation後にlowercase化する
- CodeRabbit finding 5、Major / Functional Correctness、`glm-worker/internal/parentactioncmd/parentactioncmd.go:123-126`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509323`:
  - `Pass an absolute evidence manifest path to glm-worker.`
  - parent-action cwd相対でmanifestをvalidate後、workerをrepo root cwdで起動するため別pathを読むか欠損する。`os.Stat`前にabsolute化し同じpathを`--evidence`へ渡す
- CodeRabbit finding 6、Major / Functional Correctness、`glm-worker/internal/reposearch/reposearch.go:162-177`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509329`:
  - `Return the cleaned path prefixes from resolveScopes.`
  - `clean`はvalidationだけに使われoriginal prefixを返すため、`./internal/app`や`internal//app`がvalidation後にdocument pathと一致せず、executed/zero candidatesをsilentに返す。normalized slash prefixを返す
- CodeRabbit finding 7、Major / Data Integrity、`glm-worker/internal/state/parent_action.go:165-172`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509333`:
  - `Validate parked state against ParkRecord.FromStatus before allowing unpark.`
  - `parkedActionPlan`が`pending-decision`を無視して両parent-review labelを許すため、decision-originはpendingなし、review-originはpendingありで不整合復元し得る。`ParentActionUnpark`を返す前にFromStatusとの整合を拒否する
- CodeRabbit finding 8、Major / Functional Correctness、`glm-worker/internal/state/parentevidence.go:209`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509335`:
  - `Scope the parent-evidence ledger by task and decision lease, and retain every digest.`
  - surfaceごとに1 entryを上書きするためsearch A→B→AでAを同一lease内に再投影でき、StartNewTask/Resetもledgerを除去しないため後続taskがstale digestで拒否され得る。task/lease scopeでsurfaceごとの全digestを保持し境界でclear/rotateする
- CodeRabbit finding 9、Major / Functional Correctness、`glm-worker/internal/taskdiff/identity.go:52-56,78-79`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509336`:
  - `Record the complete index and worktree identity.`
  - `git ls-files -s`の最初のblob IDだけを保持しWorktreeSHAもfile bytesだけをhashするため、mode-only changeやnon-first unmerged stage変更が同一FileIdentityとなり`reviewed(round N)`扱いされ得る。complete NUL-delimited index entries、HEAD tree-entry mode、worktree Lstat modeをhashし回帰testを追加する
- CodeRabbit finding 10、Major / Functional Correctness、`glm-worker/internal/taskdiff/identity.go:92-94`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509338`:
  - `Allow deleted paths to produce an empty worktree digest.`
  - deleted fileでは`EvalSymlinks(abs)`がENOENTとなり既存の`os.IsNotExist`処理前にreturnし、FileIdentities失敗からreviewed boundary全体を抑止する。repo rootとexisting parent pathをvalidateしfinal pathはLstatしてdeleted-fileをnew-boundary扱いする
- CodeRabbit finding 11、Minor / Functional Correctness、`IMPLEMENTATION_TASKS/105-session-rotation.md:20-23`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509341`:
  - `Mark the superseded blocked instruction as historical or explicitly supersede it.`
  - Original instructionの`blocked-user-permission` / `自動開始しない`とpermanent authorization / current ACTIVE boundaryがworker・reviewerの要求比較で競合し得るため、historicalまたはsupersededを明示する
- CodeRabbit finding 12、Major / Functional Correctness、`IMPLEMENTATION_TASKS/105-session-rotation.md:29,81`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509343`:
  - `Define deterministic predicates for every early-rotation trigger.`
  - `複数回`と`過大model-visible output`に数値閾値、measurement window、counter owner、reset ruleがなく、機械rotationとexactly-one new taskを保証できない。各predicateとdurable idempotency stateを定義する
- CodeRabbit finding 13、Major / Data Integrity、`IMPLEMENTATION_TASKS/105-session-rotation.md:31,99`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#discussion_r3945509347`:
  - `Define the parent Codex rotation boundary and preserve GLM session identity.`
  - rotationはparent Codex task/sessionだけを変更しsaved project、canonical ACTIVE/runtime state、in-flight GLM worker/reviewer sessionを維持するのか明示する。genuinely new taskはnew GLM sessionを作る。same-task resume/new-task startのlifecycle testを追加し、worker/reviewer sessionをinvalidateする`RotateInstructionSurfaceBaseline`を流用しない
- CodeRabbit observation、PR 350 summary、`https://github.com/shinderuman/codex-worker-orchestrator/pull/350#issuecomment-5562688321`:
  - Docstring Coverage warningは0.00%、threshold 80%、changed scope 168 functions / 39 files、10 unsupported。repository policyと要求へ照合し、warningだけを理由に無関係な大量docstring追加を行わない

## Purpose

固定range `43e1da9..446f318` に対するGPT proposal、Greptile full-range review、CodeRabbit単独commit reviewをcurrent authorityへ再検証し、成立するdefectだけを修正する。

## External feasibility

status: not-applicable

## Contract

- GPT branchをfetchしてproposal headをimmutable SHA `b2b3951181b9ec3afcdcfdb11b8d84cdae52f05c`と照合し、PR 349のfindingとproposal diffをlosslessに取得する
- PR 348のbase/headを指定full rangeと照合し、Greptileの全findingをlosslessに取得する
- PR 350のbase/headを指定単独commit rangeと照合し、CodeRabbit reviewがREADYになった後に全findingをlosslessに取得する
- GPT / Greptile / CodeRabbitのoriginとexact source locatorを保持してGLMへ渡し、current HEAD・Rules・本task contractに対して個別dispositionする
- GPT proposalはblind mergeせず、成立する意図だけをcurrent HEADへ適応する
- GLM処理後は独立reviewer、Sol semantic review、current snapshot validation、commit、必要なinstall/smoke、通常pushへ戻る

## Must not

- PR 348、PR 349、PR 350をmergeしない
- PR 348上のCodeRabbit結果をtransport sourceとして使わない
- PR 350がPENDINGまたはFAILEDの間にGLMへ進まない、またはfindingなし扱いしない
- transport failure、proposal SHA不一致、PR range不一致、review未完了を推測で補わない
- 外部reviewと同じsubstantive reviewをGLM前に親Codexが再実行しない
- finding統合時にoriginまたは原文を失わない

## Acceptance criteria

- GPT proposal fetch・proposal SHA、PR 348 full range、PR 350単独commit range、各review statusが指定条件と一致する
- GPT proposal diff、GPT全finding、Greptile全finding、CodeRabbit全findingがoriginとexact locator付きでtracked contractへlosslessに固定される
- 全findingにcurrent HEAD上の一意なdispositionとsource locatorがある
- 成立する各defectに原因境界を通る回帰testがある
- relevant testsとrepository quality gate、独立GLM review、Sol acceptance、commit、必要なinstall/smoke、通常pushを完了する

## Historical invariants

- PR 348、PR 349、PR 350はreview transportでありmerge対象ではない
- GLM worker/reviewerへGit remote write authorityを与えない

## Dependencies

none

## Review findings

- GPT 3件、Greptile 1件、CodeRabbit inline 13件とpre-merge observation 1件をorigin・source locator付きでResolved referencesへ固定済み

## Current boundary

全transport preconditionとreview完了を確認し、全findingを固定済み。実行可能なNEXT先頭。現在ACTIVE 105の保存済みGLM taskを変更・再起動せず、105完了後に新規taskとして開始する。
