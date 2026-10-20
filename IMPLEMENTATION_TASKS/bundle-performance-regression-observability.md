# Task: Bundleでの性能・消費量退行の継続観測

## Original instruction

2026-10-20のユーザー原文:

```text
いまってBundleに各テストの時間を出力するようになってるんだろうか
なってないなら出すようにするのは必須だと思う
```

同日の追加指示:

```text
テストに限らずいま改善しているような項目の著しい劣化が観測できるようになっているかというのがポイントだ
```

実行順と追跡についての指示:

```text
じゃあいまのタスクの直後に入れるIssueを作っておけ
```

## Amendments

### 2026-10-21 実行順指定の訂正（ユーザー原文）

```text
- [`IMPLEMENTATION_TASKS/bundle-performance-regression-observability.md`](https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/bundle-performance-regression-observability.md)

が先頭になってないじゃん

先頭ってのはActiveの次って意味じゃない

Activeにするって意味だ
```

従来のNEXT先頭という解釈は本指示により上書きされた。実行scheduleの正本はPlanのみとする。

## Resolved references

- 「いまのタスク」は`IMPLEMENTATION_TASKS/workflow-validation-cadence-and-source-binding.md`。その直後という実行priorityは`IMPLEMENTATION_PLAN.local.md`のNEXT先頭で表す。実際のcorrectness dependencyは、単に優先順が隣接することを理由として追加しない。
- 「いま改善しているような項目」は、検証の配置・回数・所要時間、個別Goテスト、Fix/Review往復、source binding、install・Publication・Retireを含む通常Workflowの費用、Codex/Sol実消費とQuality Deltaを指す。GLMの利用量は参考値であり、最上位の削減指標とはしない。

## Purpose

通常TaskのBundleとそのcanonical telemetryから、検証時間・Workflow往復・Codex/Sol消費・品質について**著しい悪化を事後に発見して原因へ辿れる**状態を成立させる。個別テスト時間の採取は必須範囲とし、観測処理自体が新たな長時間検証や親model消費を生まないようにする。

## External feasibility

status: not-applicable

## Contract

- 実装時のcurrent Gitで、Go quality gate、test runner、Codex/GLM usage telemetry、review/fix/validation、install、Publication/Retire、Bundle exportと`analysis-index`の既存producer・consumerを調べる。既に採取できる値、raw evidenceにだけ残る値、欠損する値を分け、第二正本となる収集機構を増やさない。
- **通常のGo test実行から、成功・失敗・skipを含む個別テスト（可能なsubtestを含む）とpackageの経過時間を機械可読で保存し、Bundleから取得可能にする。** `go test -json`等の実行時イベントを再利用し、計測のためだけに全suiteを再実行しない。テスト名・package・run identity・結果・elapsed、通常/raceなどの条件、実行元証拠へのlocatorを対応させる。未採取や途中終了をゼロ秒・PASSへ縮退させない。
- 各Taskの全体経過時間と主要stageのwall-clock時間、全体検証の種類・回数・所要時間・成否、重複実行、Fix/再Review・parent return・retry・limit/停止などの回数と時間を、取れる範囲で既存のTask/Attempt/run identityへ紐付ける。重複・並行実行中の時間を単純加算して全体経過時間と称しない。
- Codex/Sol側の実input/cache/output/reasoning等の利用可能な消費指標、model call/parent turn、GLM側の利用量をsourceとcoverage付きで区別する。失われたcounter、未収集、別cohortの値を推測補完せず、GLM節約をCodex Reductionと取り違えない。
- 検証source・実効入力・Go/OS・cache/parallelism・model/reasoning・計測対象と期間など、比較可能性に必要な条件を既存evidenceから結び付ける。変更前後を比較する際は条件不一致/欠損を明示し、単純な時間差を無条件の退行と断定しない。Quality Deltaに関する必要な失敗・Review証拠のlocatorも保持する。
- 単一Bundleから現状と根拠をboundedに閲覧でき、比較可能な複数Bundleまたはcanonical telemetryを使って、テスト・stage・model消費などの**著しい退行候補と寄与箇所**をdeterministicかつboundedに抽出できるようにする。閾値・noise・比較条件は代表的な実証に基づき採否判断する。比較baselineがない場合も観測事実は利用でき、比較不能はunknownとする。
- 実装は既存Bundle/telemetry/CLIのcanonical ownerに寄せる。親Codexにraw log全文や大量のテストイベントを読ませず、要約・上位悪化項目から正確な元証拠へdrill downできる構造にする。新しいmodel call、監視daemon、外部service、別の永続履歴DBを通常経路へ追加しない。
- 現ACTIVEによる検証配置の改善自体や、独立した重いテストの高速化をこのTaskで再実装しない。観測に必要な最小の実行経路の修正と検証に限定する。

## Must not

- テスト時間を観測するために通常WorkflowのQuality Gateや全suite実行を増やさない。
- 関係する値が欠損・不確定な場合にゼロ・PASS・改善扱いしない。未確認の性能削減率やQuality Deltaを主張しない。
- test/package/stageのelapsedを重複集計せず、異なるtest mode・source・environmentの測定を暗黙に同一cohortへ混ぜない。
- 生ログの大量コピー、無制限のBundle summary、model-based regression classifier、汎用監視基盤、恒常的な追加model呼び出しを作らない。
- 観測のために最終source binding、既存のevidence authority、fail-closed、必要なreview/validation/Publicationの安全境界を弱めない。
- 本Taskと異なるテストsuite削減やWorkflow再設計を、観測機構の実装に便乗して開始しない。

## Acceptance criteria

1. 通常の成功および失敗するGo test runのBundleから、**成功した個別テストも含めて**名前・package・elapsed・結果・run identity・source locatorが読める。対象testが多数でもraw log全量をmodelへ投影せず、該当テストと遅いテストを特定できる。
2. 全体検証回数/時間と各stageの時間、Review/Fix/再実行、Task wall-clockとCodex/GLM使用量について、採取できた値と欠損・unknownを区別して確認できる。重複計上のない比較条件とexact evidence locatorがある。
3. 制御された遅延・回数増加・モデル消費増加等のfixtureまたは実際の比較可能なbefore/after evidenceで、著しい退行を具体的なテスト/stage/usageへ帰属させられる。比較不可能な条件・不足したbaselineでは誤った退行/改善判定をしない。
4. 通常のBundle exportがread-onlyであり、追加の全suite実行やmodel callを必要とせず、収集・出力の増加がboundedであることを測定する。既存schema/consumerとsource bindingを維持し、根拠なしの重複実装を増やさない。
5. 関連testとrepository quality gate、必要なintegration/Bundle生成確認、独立Reviewを実施し、実観測からの所要時間・サイズ・Codex-visible出力費用と限界を報告する。

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。GLM token削減自体を最適化目的にしない。
- 既存のcanonical evidence authorityを維持し、観測結果と意味判断を混同しない。
- 通常Workflowの開発全体を高速化する目的に照らし、観測の維持費や実行時overheadが改善効果を上回る機構を導入しない。

## Dependencies

none
