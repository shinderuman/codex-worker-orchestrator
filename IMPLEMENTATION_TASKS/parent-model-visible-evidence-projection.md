# Task: Parent model-visible evidence projection

## Original instruction

````text
どう考えてもCodexのトークン消費量が多すぎる
なにか進め方がおかしい
直ちに見直せ
````

````text
さっきのGLMのリセットが回復してからのタイミングでCodexの残りも100%だった
一瞬で45%を使い果たしている
どう考えても異常だ
直ちに見直せ
````

````text
過去まで見る必要はない
このGLMリセット後が異常すぎると言っている
直ちに見直せ
````

````text
復活してからの作業がなにかおかしいのは間違いない
直ちに見直せ
````

## Amendments

### 2026-09-06

````text
この問題は絶対にNo-Goのままにするなどは許されない
最上位目標と完全に衝突するからだ
必ず根本的ハーネスを可能なら機械的に、機械的にできなくてもなにか対応を入れて再発防止策を行うこと
もし行えないならこのツールの存在意義、開発目的自体が破綻する
着手する際は最上位目標を再度確認したうえで行うこと
````

## Resolved references

- 2026-09-06のGLM rate-limit復帰後、親Codexは既知のauthority本文、review packet、handoff/finalization JSON、広いdiff/source出力をmodel-visible contextへ繰り返し展開した
- 同区間の競合統合では既review済み範囲を含む新規integration reviewも行い、変更された合成境界より広い証拠を再取得した
- 2026-09-06 06:30 JST時点のCodex 5h bucketは45%消費で、ユーザー観測では復帰時点の残量は100%だった
- 親の既存task横断`rg`は17,690 token相当を生成し、8,000 token上限で切断されたため、stdout切断だけでは消費防止にならない
- `telemetry-history-compact-summary.md`はhistory query固有であり、authority、packet、handoff、diff、source等の一般的な親model-visible evidence境界を所有しない
- 旧taskをstash後に新task開始しようとするとruntimeは`previous task is waiting for quality policy surface approval`で拒否した。`waiting-sol-review`にはstop endpointがなく`--stop`は`stop_endpoint_absent`、`--isolate`は`interrupted`限定のため、承認待ちtaskを保持した優先割込み経路がない

## Purpose

Sol Highが意味判断に必要な証拠だけを一度受け取り、既知・不変・対象外の本文や既review済み範囲を再投影しない機械境界を設け、Quality Deltaを悪化させず親Codexの実消費を削減する。

## External feasibility

status: not-applicable

## Contract

- 着手時にPlanの最上位目標「Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する」と最上位Eval「Direct Codex対Codex + glm-workerのCodex ReductionとQuality Delta」を再確認し、設計・採否・validationへ明示的に適用する
- authority取得はsnapshot/content hashを先に比較し、親が既に読んだ同一hashの本文をmodel-visible stdoutへ再出力しない。変更kindだけを一度取得できる機械projectionを持つ
- packet、handoff、finalization、validation、telemetry JSONはaction、status、digest、count、ID、exact source locator、unknown/errorだけをschema付きbounded outputへ投影し、raw全文を先に表示しない
- repository検索、diff、source抽出はsemantic questionと対象path/symbol/recordを入力必須とし、候補件数・byte/token proxy上限を超える場合は切断済み本文ではなく理由付きrefinement-requiredを返す
- 同じSol判断に必要な独立read/projectionは一つのowner call内でbatchし、完了・error・attention・追加semantic questionまで親modelへ戻さない。親が細粒度pollや逐次readを選んでもruntime側で統合または拒否する
- 競合統合やfollow-up reviewでは、既review済みblob/digestと新規合成範囲を機械識別し、継承可能なvalidation evidenceを再利用して変更境界だけをreview対象にする。digest不一致やprovenance欠落はfail closedする
- 親model-visible evidenceの種類別bytes/token proxy、parent return回数、重複digest、切断、refinement回数をtelemetryへ記録し、Direct Codex対Codex + glm-worker評価へ接続する
- Sol判断に追加本文が必要な場合は、具体的semantic questionとexact locatorを指定した追加取得を許可する
- 本FindingはGo固定とし、機械強制可能な境界を実装する。repository/runtimeだけでは強制不能な残余境界がある場合も、観測だけで終了せず、利用可能な強制・bounded fallback・検出・停止・回復策を実装して再発経路を縮退させる
- `waiting-sol-review`等の親判断待ちでもsession、task state、working tree保持基準を失わず安全に中断・isolateでき、割込みtask完了後に同じsessionへ復帰できるruntime遷移を設ける

## Must not

- 実装不能、No-Go、観測のみ、文書注意のみを本taskの終端にしない
- 品質gate、独立review、Sol semantic reviewを省略してtoken削減扱いにしない
- stdoutの後段切断だけをbounded projectionとみなさない
- hash不一致、unknown、schema欠損時にwhole-document fallbackを行わない
- 既review済みという理由だけで競合合成部分や意味変更をreview対象外にしない
- GLM token削減のために親Codexへraw evidenceを移す設計にしない

## Acceptance criteria

- 同一authority snapshotの再取得で本文model-visible bytesが0となり、snapshot/active task一致だけを確認できる
- packet、handoff、finalizationの代表fixtureで必要fieldとlocatorだけが上限内に収まり、欠損fieldはunknown/errorになる
- 広い`rg`、diff、JSON結果が上限を超えるfixtureでraw切断を返さずrefinement-requiredとなる
- 複数の独立read/projectionを含むfixtureで親model returnが意味境界の1回に集約され、細粒度poll/read要求がturn列へ展開されない
- 親判断待ちtaskから優先割込みtaskへ遷移するfixtureでNo-Goや旧task完了を要求せず、旧sessionを保持して割込み完了後に再開できる
- 既review済み変更と1件の競合合成を含むfixtureで、digest/provenanceが一致する既検証範囲を再reviewせず合成境界だけを対象化する
- 復帰後incident相当のreplayで親model-visible evidence bytes/token proxy、parent return回数、重複出力が低下し、必要なSol判断とvalidation結果が維持される
- independent reviewer、Sol semantic review、current snapshot validation、install/smokeを完了する

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaである
- 親CodexのContract違反と過大取得を通常のfault modelとして機械的に拒否する

## Dependencies

none

## Review findings

none

## Current boundary

最上位目標を再確認済み。preflightは同一sessionのbounded flowでreview・Sol承認・acceptまで完了した。親判断待ちtaskを安全にpreemptできないFindingを保持したまま、本taskをACTIVEとして直ちに実装する。
