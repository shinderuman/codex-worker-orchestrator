# Task: machine-only backward compatibilityとlegacy migrationを棚卸し・削減

## Original instruction

````text
## Task 007: machine-only backward compatibility / legacy migrationを棚卸しして削減

### User requirement

glm-worker自身・Codex/GLMだけが読むmachine dataについて、公開APIのような後方互換性を原則維持しない。

人間が過去versionを読むための互換layerを積み増す意味はない。

### 対象

少なくとも:

- GLM→glm-worker structured result
- glm-worker→Codex machine output
- resume/checkpoint JSON
- telemetry JSONL
- passive event JSONL
- timeline/convergence/status観測schema
- repo-search cache
- report-only checkpoint metadata
- Result display/text parser
- protocol state
- internal stats schema

### 現在実在するlegacy候補を必ず確認

例:

- `packet.FromDisplayLines()`による旧text PACKET→Result変換
- v2→v3 checkpoint upgrade
- old report-only checkpointをphase/suffixから推定する処理
- old stats archive compatibility
- old telemetry version skip
- `PacketCompactions`等の旧protocol専用field
- `--decision` / `--fix` argv modeを「後方互換の短文用」として残す記述
- legacy field / fallback parser / deprecated suffix inference

上記は「削除しろ」と先に決めつけず、実際のproduction用途を分類する。

ただし、

「既に実装済みだから」
「一応古いものも読めるから」

は残す理由にならない。

### 基本方針

machine-only old versionは用途に応じて:

- reject
- skip
- reset
- rebuild
- delete
- resume不能として明示終了

へ単純化。

active checkpointだけは変更時点の進行taskを壊さないよう、

- task完了後にschema変更
- old binaryでそのtaskだけ完了
- 必要ならSol判断

で保護する。

恒久migrationとは分離。

### cache

再生成可能cache:

`version mismatch → discard → rebuild`

旧cache migration禁止。

### telemetry/event log

新schemaへ変えたら新runからcurrent schemaを正にする。

過去logが必要な一回限りの分析はofflineで行い、production migration frameworkへしない。

version意味変更を同version番号内で行わない。

### current validation

後方互換を削ることとfail-open化を混同しない。

current schemaについてはstrict validationを維持。

### CLI argv compatibility

`--decision` / `--fix`を残すかは、「人間向け公開CLIとして現在も有用か」で判断する。

Codex transportがstdin modeへ統一され、実利用がなく、後方互換だけが理由なら削除候補。

ただしユーザーがterminalから短いdecision/fixを手入力する実用途があるなら、machine schema互換とは別にhuman CLI featureとして評価する。

---
````

## Amendments

### 2026-08-23 Sol review decision

````text
現案をそのまま採用しない。次の境界へ修正する。

- v2 checkpoint upgrade削除とargv `--decision` / `--fix`廃止は採用する。
- `PacketCompactions`をstats v3からversion bumpなしで削除しない。Task 008が旧protocolとの比較に使うhistorical metricであり、現時点では旧v3 archiveのdecode・aggregate・`--stats`出力を保持する。structured output移行後にproducer callが存在しないdead記録methodは削除してよいが、保存fieldの意味と既存値を消さない。将来削除するならTask 008完了後にstats versionと利用者影響を別判断する。
- `report_only` field欠落checkpointを通常auto-fix resumeへ落としてwrite-capableにしない。phase/suffix推定を削除する代わりにResumeCheckpointをv4へ上げ、v4 writerは`report_only`をfalseでも明示保存する。loaderはcurrent v4だけを受理し、旧v3以下をrouting前にresume不能として明示拒否する。これにより旧report-onlyを通常auto-fixへ誤分類しない。
- version bump前のactive checkpoint保護を確認済み。`~/.glm-worker/sessions`の全4 repo rootを確認し、codex-configとmedia-backupを含めresume checkpoint fileは現在0件、media-backupはcomplete/resume不可である。Task 007自身もterminal review状態で、同一fixは現installed v3 binaryが処理するためsource v4化を妨げない。
- checkpoint v4のexplicit `report_only`、v3 reject、report-only read-only不変条件、PacketCompactions historical出力保持をproduction testで固定する。
- README・inventory artifact・`codex/instructions/glm-execution.md`のargv mode記述を現CLIへ同期する。親instruction sourceの変更はTask 007のproduction CLI変更に伴う同一責務として扱う。
- generic migration frameworkは追加しない。
````

### 2026-08-23 Sol final review finding

````text
v4 writerが`report_only:false`を明示保存するだけでは不十分。現在のloaderはv4 JSONで`report_only` key自体が欠落してもGo boolのzero value falseとして受理し、通常auto-fixへroutingできる。これは「report_only field欠落checkpointを通常auto-fix resumeへ落とさない」とcurrent schema strict validationに反する。

generic schema frameworkを作らず、LoadResumeCheckpointでv4 `report_only` keyの存在とbool型を最小にfail-closed検証する。少なくともv4・auto-fix・report-only風phaseでkey欠落、v4・通常auto-fixでkey欠落の両方をrouting前に拒否し、worker/probe 0 call・status不変をtestする。明示falseの通常auto-fixと明示trueのreport-onlyは従来どおり受理する。README/inventoryのcurrent v4 strict contractも同期する。
````

### 2026-08-24 external output JSON/JSONL unification

````text
# Codex指示：glm-worker外部出力をJSON/JSONLへ統一する

`glm-worker` の外部出力を全量監査し、Codexなどのmachine consumerが読む情報を **JSON / JSONLの単一machine contractへ統一する**。

既存Task 007のmachine-only legacy cleanupと同じ目的なので、Task 007へ要件を反映して実施すること。重複taskは作らない。

## 最重要原則

このツールの主consumerはLLMであり、人間向けCLIとの後方互換性を維持する必要はない。

LLM側のinstructionと実装を同時に更新すれば、新しいmachine contractをそのまま利用できる。

したがって、移行のために以下を残さないこと。

- 旧text output
- 旧text parser
- JSONとtextのdual output
- `--json` / `--human` のような並行interface
- deprecated mode
- compatibility shim
- output version negotiation
- 「念のため」の旧形式fallback

古いmachine-only contractを壊してよい。

後方互換性を理由として分岐・parser・formatter・移行層を恒久的に残す方を問題とする。

今回の変更後に不要になったlegacy codeは削除する。

## 統一するcontract

原則として外部出力を次に限定する。

- 単発成功結果: JSON
- 単発失敗結果: JSON + non-zero exit code
- 継続stream: JSONL
- control event / handshake: JSONL

machine consumerが読む独自text serializationは廃止する。

JSONへ移した情報について、人間向けの再整形処理は作らない。

## 全量監査

`glm-worker` がstdout / stderrへ出すすべての情報を調査し、JSON/JSONL以外のmachine-readable outputを列挙してから変更すること。

少なくとも以下を確認する。

- 通常task結果
- `--decision`
- `--fix`
- `--resume`
- `--status`
- `--stats`
- `--timeline`
- `--convergence`
- `--watch`
- `--watch --verbose`
- `--eval-ab`
- `--verify-auto-resume`
- `--reset`
- `--accept`
- argument / usage error
- runtime error
- PTY stdin-ready handshake
- その他stdout / stderrへの直接出力

grepだけでなく、実際のproducerとconsumerを追跡すること。

## 削除対象

以下のようなpresentation専用処理は、意味処理と分離したうえで不要なら削除する。

- `KEY: value`
- `yes/no` 文字列化
- `none` / `unknown` のpresentation用文字列化
- mapの `a=1,b=2` 化
- sliceの区切り文字join
- ASCII graph
- `LIVE ...` 系独自行
- 複数fieldを人間向け1行へ合成する処理
- JSONLをparseした後に再度独自textへ変換する処理

bool、number、array、object、null等はJSONの型をそのまま利用する。

意味のある集計・validation・redaction・duration計算・classification等は維持する。

## `--status`

`--status` 自体をJSON contractへ変更する。

`--status --json` は追加しない。

現在の `KEY: value` contractと、その出力専用formatterは削除する。

Codex instructionもJSON fieldを読む契約へ同時に変更する。

## `--stats` / `--timeline` / `--convergence`

集計結果をtyped structとして返し、そのままJSON encodeする。

ASCII graphや文字列化mapなど、LLMが再解釈するだけのpresentationは削除する。

## `--watch`

元eventがJSONLなら、JSONLをtextへ潰して再表示しない。

watch固有情報が必要なら型付きeventとしてJSONLへ追加する。

`--watch --verbose` の `LIVE ...` 系出力もJSONL eventへ統合する。

stream内で複数の独自text protocolを混在させない。

## error

machine consumerが処理するerrorも構造化する。

最低限、

- error code / kind
- message

をJSONとして出力し、process failureはexit codeでも示す。

人間向けusage文を別contractとして維持しない。

## PTY handshake

`GLM_STDIN_READY` の固定文字列protocolも監査対象とする。

handshake自体は維持するが、独自serializationである必要がなければJSONL control eventへ統合する。

race修正で成立したREADY-before-writeの意味契約は絶対に壊さない。

## consumer更新

producerだけ変更して終わらせない。

repository内で旧形式を読んでいる、

- Codex instruction
- Go code
- shell
- test
- fixture
- documentation
- task contract

を検索し、新contractへ同時に置換する。

旧形式を読むconsumerがなくなったことを確認してlegacy parserを削除する。

## テスト

各commandについて、意味的に重要な出力contractをtyped JSONとして直接検証する。

文字列全体のgolden testを大量に作らず、

- required field
- JSON type
- enum
- null / omission
- status別field
- error contract
- JSONL event type
- control event ordering

など実際のcontractを検証する。

さらにrepository全体を検索し、廃止した旧machine protocolのproducer / parser / formatter / fixtureが不要に残っていないことを確認する。

`gofmt`、`go vet`、全test、race、buildを通す。

## scope

新しい汎用CLI framework、serialization abstraction、compatibility frameworkは作らない。

標準 `encoding/json` と用途別の小さなoutput structを基本とする。

今回の目的は機能追加ではなく、

**machine interfaceをJSON/JSONLへ一本化し、不要になったpresentation・legacy・compatibility codeを削除して構造を単純化すること**

である。

現在のACTIVE taskに別の未コミット変更がある場合は混ぜない。

GLMにcommit/pushさせない。\
pushしない。
````

## Purpose

parser/state分岐とescaped surfaceを削減しcurrent contractへ収束する。

## Contract

- legacy候補を現在必要/active task一時保護/削除/skip-reset-rebuildへ根拠付き分類
- schema意味変更はversionを上げ、同version内の意味driftを作らない
- current validationはstrict fail closedを維持
- 外部単発結果/失敗をtyped JSON、継続streamとcontrol/handshakeをtyped JSONLへ統一し、独自text machine serializationを残さない
- 全commandのproducerとrepository内consumerを追跡し、consumer更新後に旧formatter/parser/fixtureを削除する
- bool/number/array/object/nullはJSON型を維持し、意味集計・validation・redaction・duration・classificationはpresentation削除と分離して保持する

## Must not

- 既存実装や後方互換だけを残存理由にしない
- machine schema互換とhuman CLI featureを混同しない
- active checkpointを無断破壊しない
- JSON/text dual mode、human/deprecated mode、version negotiation、compatibility shim、旧text fallbackを追加しない
- READY-before-writeのPTY順序契約を壊さない
- 汎用CLI/serialization frameworkへ拡張しない

## Acceptance criteria

- 全対象inventory artifact
- 不要parser/migration/fallback/推定削除
- mismatch方針とactive task保護をproduction/testで固定
- code/branch量変化を測定可能にする
- 通常結果、全列挙command、error、usage、PTY handshake、watch streamの外部出力がJSON/JSONL単一contractになり、旧machine text producer/consumer/formatter/parserが残らない
- required field・JSON type・enum・null/omission・status別field・error・JSONL event type・control orderingをtyped contract testで固定する
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- v2/v3 checkpoint、structured output、telemetry version、repo-search cacheの履歴見出し

## Dependencies

none

## Review findings

none

## Current boundary

前段のmachine-only legacy削減はcommit `029c6f8`で局所完了したが、ユーザーが同一Task 007へ外部出力全体のJSON/JSONL統一を追加したため再open。task ID `820e121c-4a18-4c41-b644-86d18a850896`のworker-new開始後にZ.ai 5h rate limit停止し、同一session/checkpointをautomation `glm-worker-resume-4b1083bd6f6e-820e121c`で`2026-08-24T06:15:56+08:00`へ自動再開する。
