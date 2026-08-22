# Task: 複数repositoryのworkflow telemetryから改善施策を判断する

## Original instruction

````text
じゃあtelemetryを計測して改善施策を考えてくれ
これはいまのK3だけの話ではない

むしろK3はいったん置いといていい
今やれる範囲のTelemetryの計測をその内容から改善施策を考えて作業に追加してくれ
改善施策がなければそれでもいい

それとは別にチャットの返信としていまのK3の話は意味がありそうかを返信してくれ

あと media-backupリポジトリでも少しGLM-Workerを使用しているのでそちらのほうも見るように
````

## Amendments

- 2026-08-23 user correction:

````text
GLMの消費を削減しようとしているか？
GLMの消費が少ないに越したことはないが、最上位目的は
Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。
ことだぞ
````

## Resolved references

- `K3`は、直前に検討したOpenCode GoのKimi K3をGLM系列の後段reviewerとして任意追加する案を指す。本taskでは導入判断・実装を保留する。
- `media-backupリポジトリ`は`/Users/shinderumanm/src/media-backup`を指す。
- `Telemetry`は`glm-worker --stats`と両repositoryに対応する保存済みglm-worker telemetryを指す。

## Purpose

現在利用できる複数repositoryの保存済みtelemetryから、Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減できる具体的な根拠があるかを評価し、Codex ReductionとQuality Deltaへ接続できる施策だけを実装計画へ反映する。

## Contract

- codex-configとmedia-backupを別母集団として計測し、task / phase / role / session / model別のcall数、turn、token proxy、所要時間、fix・resume・rate limit・packet終端を追加AI callなしで比較する
- 最上位評価軸はDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaとし、GLM token / turn / duration削減を成功条件や優先順位の根拠へ単独使用しない
- GLM消費はprovider枠、wall time、運用継続性、Codex削減を得るための総costという二次制約として併記する
- telemetry coverage欠損、小標本、task難易度・risk差を明示し、相関を原因や効果として断定しない
- Codex差し戻しを直接表す既存signalの有無を確認し、存在しないmetricを推測値で代替しない
- Codex actual usageが現telemetryで取得不能ならunknownとし、Codex-facing packet bytesや親action回数をactual usageへ読み替えない
- 既存fixed Evalとpermission待ちのlive Direct/orchestrated A/Bを区別し、無許可A/Bなしで今確定できる施策だけを採用する
- 既存Task 008 / 009 / 011との重複を整理し、改善根拠がある場合だけ既存task amendmentまたはsemantic task追加として具体化する
- Kimi K3は今回の施策候補から外し、現在のworker / reviewer / Codex経路で実施可能な改善を優先する

## Must not

- benchmark目的の追加AI call、実Sol/Codex本番A/B、Kimi K3導入を行わない
- raw prompt / response / command本文、秘密、高cardinality情報を新規保存・表示しない
- 小標本のmedia-backupや不完全coverageのcodex-configから一般的なmodel優劣を断定しない
- GLM call・turn・token・durationが大きいことだけを理由に、task分割、review縮小、model downgrade、hard capを優先しない
- 改善施策が見つからない場合に、新metric・新機構を成果のためだけに追加しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- 両repositoryの再現可能なstats基準値とcoverage制約を記録
- 保存済みtelemetryからrole / model / task / phase別の主要分布とoutlierを追加AI callなしで比較
- Codex差し戻し・GLM内fix・reviewer終端を区別し、直接観測できない境界を明示
- Codex-facing bytes / parent touchpoint / actual Codex usage / Quality Deltaを別metricとして扱い、proxyとactualを混同しない
- 改善候補ごとに根拠、期待効果、品質risk、必要metric、採用・保留・棄却を提示
- 採用施策だけを重複のない既存task amendmentまたは新taskへ反映し、施策なしも正当な結論として許容
- 必要最小限のreview、親Codex commit、push禁止

## Historical invariants

- 2026-08-21 canonical telemetry分析
- Task 008 machine protocol measurement、Task 009 worker call outlier、Task 011 operation category telemetryは既存の個別責務として維持する

## Dependencies

none

## Review findings

none

## Current boundary

2026-08-23の追加AI callなし計測で次を確認。codex-configは17 task / 62 model call（raw current record 58、historical gap 4）、media-backupは4 task / 8 model call（coverage complete）。codex-configのworkerはmodel時間46,591,678ms / 全体57,287,575ms、worker-new turn中央値113 / p95 203だが、これはCodex Reductionの直接証拠ではない。`fix_commands=11`は7 taskに分布するがoriginを判別不能。convergenceはcodex-config 23 round中semantic 19、doc 2、verification-only 1、same-snapshot 1、media-backup 4 round中semantic 2、same-snapshot 1、verification-only 1。Codex actual usageとQuality Deltaは現telemetryだけでは測定不能。Task 009 / 010前倒しは撤回し、parent review outcome telemetryだけをCodex側観測改善候補として採用。review縮小・model変更・Kimi K3は証拠不足で保留。最終reviewとcommitは未完了。
