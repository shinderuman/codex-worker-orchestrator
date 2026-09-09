# Task: External review PR 354 / GPT proposal PR 355 intake

## Original instruction

````text
EXTERNAL\_REVIEW\_INTAKE

range: 4d8bab88b4ad59f3483cd98422ad09fe4cac553e..0266c4b7977edfdd8f832dca8bd6b247572c21b7

gpt\_review:\
status: PROPOSAL\
pr: [https://github.com/shinderuman/codex-worker-orchestrator/pull/355](https://github.com/shinderuman/codex-worker-orchestrator/pull/355)\
branch: gpt-review/4d8bab8-0266c4b\
proposal\_head: 22a6c30aec5681bbd0257be0a5ab15c6daba60a3

external\_review:\
pr: [https://github.com/shinderuman/codex-worker-orchestrator/pull/354](https://github.com/shinderuman/codex-worker-orchestrator/pull/354)\
base\_sha: 4d8bab88b4ad59f3483cd98422ad09fe4cac553e\
head\_sha: 0266c4b7977edfdd8f832dca8bd6b247572c21b7\
coderabbit: READY\
greptile: READY

Codex:

- current authorityを再確認する
- GPT proposalがあればfetchしてproposal\_headを確認する
- External review PRのrangeを確認する
- CodeRabbit / Greptile双方のreview完了を確認する
- GPT / CodeRabbit / Greptileの全findingをlosslessにGLMへ渡す
- 同じsubstantive reviewをGLM前に再実行しない

GLM:

- current HEAD / Rules / relevant task contractに対して全findingを検証する
- 成立する問題を修正し必要なtestを実行する

transport failure、range不一致、外部review未完了時はGLMへ進まない。
````

## Amendments

- 2026-09-09: task metadataのtoken overheadを削減するため、埋め込みraw intake payloadを除去し、canonical locatorから必要時に再取得する形へ変更する。

### 2026-09-09 clarification

````text
Original Instructionの原文保持に間違いはなかったので原文に戻せ
````

### 2026-09-09 token guard

````text
トークンを無駄しないためにJSONを削除しろと言ったんだがこれじゃあ作業時にまたJSONを取得することになるんじゃないか？
せめて作業時に取得したJSONを精査してJSONを保存しないみたいな内容をガードとして入れろ
一番気にしているのはトークンの無駄な消費だ
逆に言えば無駄に消費しないのなら巨大なJSONを残していい
````

## Resolved references

- [GPT proposal PR 355](https://github.com/shinderuman/codex-worker-orchestrator/pull/355): fetched local ref `refs/external-review/pr355-proposal`, verified head `22a6c30aec5681bbd0257be0a5ab15c6daba60a3`
- [external review PR 354](https://github.com/shinderuman/codex-worker-orchestrator/pull/354): verified range `4d8bab88b4ad59f3483cd98422ad09fe4cac553e..0266c4b7977edfdd8f832dca8bd6b247572c21b7`
- CodeRabbit review and Greptile check were complete for the verified PR 354 head at intake time

## Purpose

指定rangeに対するGPT proposal・CodeRabbit・Greptileの外部review結果を、外部成立性gateを満たした場合だけlosslessにGLMへ渡し、current authorityとcurrent HEADに対して成立するfindingだけを修正する。

## External feasibility

status: not-applicable

## Contract

- GPT proposal PR 355をfetchし、proposal branchのheadが指定`proposal_head`と一致することを確認する
- external review PR 354のbase/headが指定SHAと一致し、review対象rangeが完全一致することを確認する
- CodeRabbitとGreptile双方のreviewが完了済みであることをGitHub上の一次情報から確認する
- ACTIVE化時にPR 354 / 355のcanonical GitHub sourceを1回のowner tool orchestration内で取得し、raw JSONをmodel-visible outputへ出さずmemory内でschema検証・source分類・件数closureまで行う
- raw JSON responseは永続化せず、各findingの本文、source URL、reviewer/source、対象path・line・diff hunk等の判断に必要なcontextだけをlosslessな正規化text artifactへ投影する。transport metadata、avatar、node metadata等の判断不要fieldはartifactへ入れない
- source別の取得件数と正規化artifact内のfinding件数を機械照合し、未分類item、本文欠落、件数不一致、projection失敗があればGLMへ進まない
- 親Codexへ返す取得結果はgate、source別件数、canonical locator、artifact path・digestだけのbounded manifestとし、raw responseや全finding本文を親model contextへ再投影しない
- GLMには正規化text artifactだけを読ませ、raw GitHub JSONをtask file・USER_REQUEST・packetへ複製しない
- CodexはGLM委譲前に同じsubstantive code reviewを再実行せず、transport/range/completion/finding収集の機械確認だけを行う
- GLMはcurrent HEAD、Rules、このtask contractに照らして全findingの成立性を検証し、成立する問題だけを修正して必要なtest・lint/build・自己reviewを行う

## Must not

- transport failure、proposal head不一致、external review range不一致、CodeRabbitまたはGreptileのreview未完了状態でGLMへ進まない
- proposal branchをblind apply、merge、cherry-pickしない
- 外部findingを要約で置換、重複除去、黙って脱落させない
- raw GitHub API JSONをtask file、parent-visible stdout、GLM packet、保存artifactへ書き出さない
- raw JSONを取得後の別turnへ持ち越したり、正規化後の確認目的でwhole responseを再表示したりしない
- GLM worker/reviewerへGit remote write authorityを与えない
- 現ACTIVEの未完了実装へこのreview修正を混在させない

## Acceptance criteria

- GPT proposal head `22a6c30aec5681bbd0257be0a5ab15c6daba60a3`をfetch済みrefのOIDで確認できる
- external review PR 354のbase/headが`4d8bab88b4ad59f3483cd98422ad09fe4cac553e` / `0266c4b7977edfdd8f832dca8bd6b247572c21b7`と一致する
- CodeRabbit / Greptile双方のreview完了を確認できる
- 3 sourceの全findingをcanonical locatorから取得し、raw JSON非保存・model-visible非表示のまま正規化text artifactへlosslessに投影する
- source別取得件数とartifact収録件数が一致し、未分類・本文欠落が0件であることを機械確認できる
- 親model-visible結果がgate・件数・locator・artifact path/digestだけにboundedされ、raw JSONと全finding本文を含まない
- GLMが全findingをcurrent authority/current HEADに対して採否判定し、成立findingの修正と必要testを完了する
- independent reviewer、Sol semantic review、relevant/full validation、必要なruntime install/smokeを完了する

## Historical invariants

- parent-managed implementation metadataは親Codexだけが編集する
- GLM worker/reviewerにGit remote write authorityを与えない

## Dependencies

none

## Review findings

intake gate: ready

- GPT proposal: PR 355、verified head `22a6c30aec5681bbd0257be0a5ab15c6daba60a3`、14 commits・1 comment
- CodeRabbit: PR 354上の1 review・2 summary comments・5 inline findings
- Greptile: PR 354上の1 review comment・1 successful check
- raw本文・GitHub response JSONはtask fileへ埋め込まない。ACTIVE化時の取得は同一tool orchestration内で機械parseし、raw JSONを保存・表示せず、全finding本文だけをlosslessな正規化text artifactへ投影する。件数closure、head/range/completion、transportのいずれかが不成立ならGLMへ進まない。

## Current boundary

fetch・range・CodeRabbit / Greptile completion gateはintake時点で成立済み。現ACTIVE `IMPLEMENTATION_TASKS/generic-repository-harness-boundary.md`完了後に本taskをACTIVE化し、PR 354 / 355のraw JSONをmodelへ返さない単一tool orchestrationで全findingを正規化text artifactへ投影する。bounded manifestと件数closureを確認後、artifactだけをGLMへ渡してcurrent HEAD / Rules / task contractに対する検証・修正へ進む。Codexは同じsubstantive reviewを先行実行しない。
