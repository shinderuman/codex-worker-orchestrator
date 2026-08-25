# Task: Markdown runtime context footprintを削減する

## Original instruction

`````text
# Codexへの指示：Markdown肥大化とcontext消費を根本修正する

このリポジトリではREADME・EVAL・Codex instruction・IMPLEMENTATION管理Markdown等が増殖しており、親Codex / worker / reviewerが大量のMarkdownを繰り返し読むことでcontextを急速に消費し、Compactionを早めている可能性がある。

単にREADMEを短くするのではなく、**Markdown全体の責務・runtime読込経路・重複・再読規則・文章を固定しているtestまで監査し、必要な情報を失わずcontext footprintを縮小すること。**

これは文書整理taskではなく、**GLM-Workerの実行効率・Codex context消費を改善するruntime設計task**として扱う。

## 最重要原則

以下を混同しない。

```text
README.md
    人間の利用者向け入口

EVAL.md
    Eval仕様・escaped case・評価根拠

codex/AGENTS.md
    Codex instruction routing

codex/instructions/*.md
    親Codexのruntime contract

codex/instructions/worker/*.md
    worker/reviewer向け必要時instruction

codex/glm-worker/prompts/*.md
    worker/reviewer production prompt

IMPLEMENTATION_RULES.md
    このrepository開発時の長期不変ルール

IMPLEMENTATION_PLAN.local.md
    現在状態/index

IMPLEMENTATION_TASKS/*.md
    task requirement正本

IMPLEMENTATION_HISTORY.md
    過去証跡・escaped原因・完了履歴
```

各surfaceに同じ説明を重複して書かない。

「念のため別Markdownにも追記する」をデフォルトにしない。

新しい文章を追加する場合は必ず、

1. 既存記述を置換できないか
2. 統合できないか
3. 古い記述を削除できないか
4. そもそもそのsurfaceへ書く必要があるか

を先に判断する。

## Markdownのownership

Markdownを一律に同じ種類の文書として扱わない。

### 親Codex専有

以下は親Codexが管理するcontrol-plane / requirement metadataであり、GLM worker / reviewerは編集・生成・削除・復元しない。

- `IMPLEMENTATION_RULES.md`
- `IMPLEMENTATION_PLAN.local.md`
- `IMPLEMENTATION_TASKS/*.md`
- `IMPLEMENTATION_HISTORY.md`

これらは「何を実装するか」「現在どこまで進んでいるか」を監督者が定義するsurfaceである。

GLMが自分の実装結果に合わせて要求・状態・履歴を書き換える自己充足を許可しない。

変更が必要な場合、GLMは変更候補をstructured resultとして親Codexへ返し、親Codexが判断・編集する。

### GLMがtask範囲内で編集可能

以下はGLM-Worker製品そのもののsourceであり、ACTIVE taskの要求を満たすために必要ならGLMが編集してよい。

- `README.md`
- `EVAL.md`
- `codex/instructions/**/*.md`
- `codex/glm-worker/prompts/**/*.md`
- その他product documentation

ただし、Markdownはコード変更に付随して自動的に更新する対象ではない。

以下の場合だけ変更する。

- ACTIVE taskの要求・acceptance criteriaが変更を要求する
- production contractとの整合性維持に必要
- 既存記述が変更後の挙動と明確に矛盾する

「関連している」「念のため説明を足す」だけでは変更しない。

変更時は新規追記より、既存記述の削除・置換・統合を先に検討する。

### executable specification

`codex/instructions/**` と `codex/glm-worker/prompts/**` は文章だがagent behaviorを変更するexecutable specificationとして扱う。

これらの意味変更は通常のREADME編集より高リスクとし、独立reviewerと親Codexによる意味確認を必須とする。

## まず現状を計測する

修正前にrepository内の全Markdownを列挙する。

最低限、

* path
* line数
* byte数
* 可能ならtoken数またはtoken概算
* 主な読者
* runtimeで読むか
* どのeventで読むか
* 誰が読むか（親Codex / worker / reviewer / human / test）
* 全文readか部分readか
* 他Markdownとの重複

を一覧化する。

特に以下の実行経路について、**実際のinstruction routingをコード・AGENTS・promptから追うこと。**

```text
NEW_TASK
通常resume
Compaction後resume
provider/rate-limit後resume
worker → reviewer
review差戻し
Sol判断後resume
commit
install
外部成立性確認
```

推測で「読んでいるはず」と判断せず、production prompt / AGENTS routing / workflow codeを一次証拠として確認する。

既存session log・telemetryからcontext膨張の証拠を取得できる場合は利用する。

ただし、この調査だけのために追加のlive Codex / GLM Evalを大量実行しない。既存証拠と静的read graphを優先する。

## Compaction feedback loopを監査する

特に現在の、

```text
Markdownを大量に読む
↓
context増大
↓
Compaction
↓
Compaction後なのでMarkdownを再読
↓
context増大
```

というfeedback loopが成立していないか確認する。

`IMPLEMENTATION_RULES.md`、`IMPLEMENTATION_PLAN.local.md`、ACTIVE task file、History、Codex instruction群について、

**Compaction後に本当に全文再読が必要なのか**

をsurfaceごとに再評価する。

ただしrequirement保全を弱めてはいけない。

## IMPLEMENTATION管理Markdown

現在成立している4層構造そのものは維持する。

```text
IMPLEMENTATION_RULES.md
IMPLEMENTATION_PLAN.local.md
IMPLEMENTATION_TASKS/*.md
IMPLEMENTATION_HISTORY.md
```

Planを再び巨大仕様書へ戻さない。

History全文を通常resume時に読ませない。

### RULES

`IMPLEMENTATION_RULES.md`には長期不変invariantだけを残す。

個別incident、過去task説明、重複した手順説明を置かない。

### PLAN

`IMPLEMENTATION_PLAN.local.md`は現在状態/indexだけとする。

個別task詳細を再掲しない。

### TASK

task fileはrequirement正本なので、ユーザーOriginal instruction・Amendments等を勝手に要約削除しない。

ただし、**「正本として保存すること」と「毎回全文をcontextへ入れること」は別問題**として扱う。

現在の再読contractを監査し、

* task開始時
* user amendment受領時
* requirement不整合時
* final independent review時

など、Original instruction全文との照合が必要なeventと、

* ordinary resume
* provider resume
  -単純なworker継続

で必要なread setを区別できるか検討する。

resume時に必要十分なsectionだけで安全に成立するなら、全文再読をやめる。

ただし新しいsummaryをrequirement正本へ昇格させない。

Compaction summaryやconversation memoryを正本に戻してはいけない。

### HISTORY

Historyは明示参照された必要箇所だけ読む。

「念のため全文を読む」経路が存在するなら除去する。

## Codex instruction群

`codex/AGENTS.md`は**routing/index**へ徹する。

個別contract本文をAGENTSへ重複コピーしない。

`codex/instructions/*.md`も責務ごとに整理し、一つの通常taskで不要なinstructionまで連鎖的に読む構造を減らす。

特に以下を監査する。

```text
glm-execution.md
task-lifecycle.md
git.md
feasibility-gate.md
failure-evidence.md
escaped-cause-layer.md
direct-edit.md
glm-auto-resume.md
```

例えば、

```text
AGENTS
→ glm-execution
→ task-lifecycle
→ 別instruction
```

のような読込連鎖について、本当にそのeventで必要か確認する。

「関連しているから読む」ではなく、**その時点の判断に必要だから読む**ものだけroutingする。

共通説明を各instructionへコピーしない。

新しいMarkdownを追加して問題を分割しただけで総読込量が増える変更は禁止する。

## README

READMEは人間向け入口へ戻す。

READMEに必要なのは概ね、

* 何のtoolか
* 基本architecture
* install
* 最低限の利用方法
* 主なCLI
* 詳細情報への入口

だけ。

内部state machine、escaped incident、Eval contract、細かなscheduler contract、内部安全gate、開発履歴等を網羅的に再掲しない。

runtime contractの第二正本にしない。

READMEの短縮は行うが、README短縮だけで本taskを完了扱いしない。

## EVAL.md

EVAL.mdはREADMEとは別物なので、単純な短文化対象にしない。

必要なEval contract、positive/negative case、escaped原因との対応は維持する。

一方、

* 同じincident説明の重複
* instruction本文の長文コピー
* test実装詳細の過剰な再掲
* chronological append-only accumulation

が存在する場合は整理する。

EVALは通常runtimeで全文readしないことを保証する。

## proseをtest ABIにしない

今回の重要な監査対象。

現在、Go testや`install_smoke.sh`等でMarkdown中の**長い説明文そのものを`strings.Contains` / `grep -F`等で固定している箇所**が存在する。

これにより、

```text
文章を簡潔化したい
↓
testが壊れる
↓
既存文を消さず新しい文を追記
↓
Markdownがappend-onlyで肥大化
```

という圧力が発生していないか確認する。

人間向け説明文をAPIとして固定しない。

exact text pinが必要なのは、

* protocol literal
* machine-readable keyword
* 必須heading / stable identifier
* external interfaceとして文字列自体に意味があるもの

に限定する。

単なる日本語説明文をtest contractにしている箇所は、可能な限り、

* production behavior test
* routingの存在確認
* 短いstable identifier
* heading / rule ID
* structured contract

等へ置換する。

このためだけに巨大なMarkdown parser/frameworkを新設しない。

文章の言い回しを変えただけで大量test修正が必要になる構造を解消する。

## コメント肥大化と同じ品質原則を適用する

今回のMarkdown問題は、コードコメントの過剰生成と同じ上位原因として扱う。

AIが、

```text
新機能
→ 説明追加
→ 既存説明維持
→ 別surfaceにも説明追加
```

を繰り返さないようにする。

既存reviewerのコメント品質contractと同様に、Markdownについても、

* 既存内容の言い換えだけ
* 他fileの説明コピー
* 実装を読めば分かる低価値説明
* 同じ理由を複数箇所へ記載
* 完了incidentを恒久instructionへ残す

をreview対象とする。

ただし「文章を短くすること」自体を目的化せず、理由・制約・invariant・外部仕様等の必要情報は残す。

## 実装後の計測

修正前後で、最低限以下を比較する。

```text
全Markdown総量

親Codex:
  NEW_TASK開始時read量
  ordinary resume時read量
  Compaction後resume時read量
  review差戻し後read量
  commit時read量

worker:
  task開始時instruction量

reviewer:
  review開始時instruction量
```

可能ならbyte / token双方を出す。

「Markdown総量が減った」だけではなく、**runtimeで実際に読む量が減ったこと**を成果条件とする。

総Markdown量が多少残っても、runtime cold-pathへ退避できていればよい。

逆にREADMEだけ大量削除してparent Codexのread量が変わらなければ成果とはみなさない。

## regression防止

必要なtestを追加・修正し、

* requirement source-of-truthを失わない
* Compaction後に誤ったtaskへ進まない
* reviewerがOriginal instructionとの独立照合を必要な時点で維持する
* Historyを通常全文readしない
* eventに無関係なinstructionを不要にloadしない
* README/EVALがruntime requirement sourceにならない
* proseのappend-only growthをtestが強制しない

ことを確認する。

## 禁止事項

* requirement保全を犠牲にして単純にMarkdownを削る
* Original instructionをsummaryへ置換する
* conversation memoryを正本へ戻す
* Planを再肥大化する
* 新しい巨大な「全部入りMarkdown」を作る
* Markdownを分割しただけでruntime総read量を増やす
* 新しい一般Markdown parser/frameworkを作る
* token節約目的で安全gate自体を削る
* README削減だけで完了する
* live Codex/GLMを大量消費するbenchmarkをユーザー許可なしで実行する

## 完了条件

最終的に、

> **必要なcontractは保持したまま、親Codex / worker / reviewerが通常作業で読むMarkdownを必要最小限にし、Compaction後の再読がさらなるCompactionを誘発する構造を解消する**

こと。

修正後に、

1. Markdown responsibility map
2. runtime read graph
3. 修正前後のread量
4. 削除・統合した重複
5. 意図的に残した長文と、その理由
6. prose exact-pinを廃止または維持した箇所と理由

を報告すること。
`````

## Amendments

none

## Resolved references

- 「このリポジトリ」は`codex-worker-orchestrator`を指す。
- 「現在の再読contract」は、本task開始時点のAGENTS・IMPLEMENTATION_RULES・各Codex instruction・production prompt/wiringで成立しているread routingを指す。

## External feasibility

status: not-applicable

## Purpose

requirementのlosslessな正を維持しながらMarkdown surfaceの責務、runtime read graph、重複、prose exact-pinを整理し、親Codex・worker・reviewerが通常loopで読む固定contextを削減する。

## Contract

- 全Markdownをreader・event・read範囲・重複込みでinventoryし、production routingの一次証拠からruntime read graphを作る。
- README/EVAL/Codex instruction/worker instruction/prompt/IMPLEMENTATION 4層の責務を分離し、同じcontractを複数surfaceへ複製しない。
- lossless Original instructionとsource-of-truth順位を維持し、ordinary resume等で本当に必要なread setだけへ縮小できるかevent別に判断する。
- Compaction後の再読が次のCompactionを誘発するfeedback loopを計測し、requirement正本をsummaryへ置換せず解消する。
- Markdown proseを固定するtestを監査し、protocol literal・stable identifier等でない長文exact-pinをbehavior/route/短いIDの検証へ置換する。
- 修正前後でMarkdown総量だけでなく親Codex・worker・reviewerのevent別runtime bytes/token proxyを比較する。
- executable specificationの意味変更は独立reviewと親Codex確認を通す。

## Must not

- requirement保全、Original instruction、source-of-truth、safety gateをcontext削減のために弱めない。
- Planを巨大仕様書へ戻さず、新しい全部入りMarkdownや一般Markdown parser/frameworkを追加しない。
- Markdownを分割しただけでruntime総read量を増やさない。
- README短縮や総Markdown量削減だけで完了扱いにしない。
- conversation memoryやCompaction summaryをrequirement authorityへ昇格しない。
- ユーザー許可なしにlive Codex/GLMを大量消費するbenchmarkを行わない。

## Acceptance criteria

- Original instructionの全完了条件を満たす。
- Markdown responsibility mapと実producerに基づくruntime read graphを作成する。
- NEW_TASK、ordinary/Compaction/provider resume、worker→reviewer、review差戻し、Sol判断後resume、commit、install、外部成立性確認のread量を修正前後で比較する。
- requirement source・reviewerのOriginal instruction照合・History cold-path・Plan index性を維持する。
- README/EVALがruntime requirement sourceにならず、無関係instructionの連鎖readを削減する。
- prose append-only growthを促すexact-pinを廃止または正当化し、変更箇所と理由を報告する。
- 削除・統合した重複、意図的に残した長文と理由、bytes/token proxyの改善を報告する。
- 必要なtest、独立review、必要なSol gate、親Codex最終reviewを通す。
- GLMはcommit/pushせず、完了gate後のlocal commitは親Codexが行う。pushしない。

## Historical invariants

- 最上位目的は、Sol High相当の品質を可能な限り維持しながらCodex/Sol実消費量を大幅に削減すること。
- IMPLEMENTATION 4層とlossless requirement sourceは維持する。
- 親Codex 5h Limit自動再開は先行taskで実装・review・実機PoC・full install smokeを完了しており、本taskはそのCodex instructionを通常runtime read graphの監査対象に含める。

## Dependencies

none

## Review findings

none

## Current boundary

ユーザー指定の5h Limit自動再開taskは完了済み。Greptile低コストscheduled dispatchの後に実施するNEXT。未着手。
