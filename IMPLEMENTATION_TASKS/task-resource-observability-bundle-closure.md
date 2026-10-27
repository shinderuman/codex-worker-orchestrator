# Task: 通常Taskの実行中リソース観測を実際のDogfood Bundleまで接続し、前Taskの未達を修正する

## Original instruction

```text
おまえBundleを生成してみろ
そしてファイルを確認しろ
前タスクの要件を満たしてねえぞ
いまはすでに別のタスク行ってしまっているから現タスクの次に修正する
いま復活させると面倒なことになるから`bundle-host-resource-observability.md` の内容を直接俺に渡せ
```

```text
いや回収したものを出せってことじゃねえよお前がミスしたものを直すからそれ用のものを作れって意味だよ
```

```text
なんでBundle生成すればわかるようなことを確認してなかったの？
なんで生成すればわかるのにタスクは完了しているって言ったの？
```

## Amendments

none

## Resolved references

- 前Taskの要求正本: Git object `4840d21b302fd84582681c003ea7df53aa44709e^:IMPLEMENTATION_TASKS/bundle-host-resource-observability.md`。元のOriginal instruction、Contract、Must not、Acceptance criteriaを読み、本Taskの修正条件と併せて満たす。削除済みの現物や会話の要約から原要求を推定しない。
- 前Taskの実装コミット: `4840d21b302fd84582681c003ea7df53aa44709e`。
- ユーザー指摘後に通常の `glm-parent-action export-bundle` で生成・確認した反証Bundle: `/Users/shinderumanm/.glm-worker/exports/f6189a2ef4b9698296980938dcb3f04542eadb0638397e403f7925130b92cfc4/7541c945-4f90-4b7c-a344-a4736623ac63.zip`。対象は `IMPLEMENTATION_TASKS/worker-go-test-docker-routing.md`。54 entries中、`resource-samples.json`、Quality Gateの`run.json`、`test-timings.json`はいずれも0件。`manifest.json`と`analysis-index.json`にもresourceの参照はなかった。この事実だけから、インストール・収集・保存・exportのどこが原因かは確定しない。
- 前回の親検証は、実ホストのsamplerを直接起動し、採取結果をテストfixtureの保存先へ書き込んでexportするものだった。通常Taskの実行から保存・Bundle収録までの接続を確認した証拠にはならない。
- 前回の親は、上記の局所検証、独立Review、最終Gate、completion処理の成功をもって完了と判断したが、通常経路で生成したBundleの現物確認を行っていなかった。「次回taskから収録される」という説明も未検証だった。
- 待機コストに関するユーザー原文: 「いや、全体で2.5秒を1回だけ待つのであればそれは問題ないと思うが」「2.5秒を何十回何千回と待つならそれは問題だと思う」。個別テスト、テレメトリ生成、Bundle生成の反復に固定待機を加える設計の許可ではない。
- 採取方式に関するユーザー確認: 「あとなんか文字列をパースしているみたいなことをさっき言っていたが標準ライブラリでCPU情報とかは取れなかったか？」。前Taskでは標準Goのプロセス統計とホスト統計を区別し、macOSのコマンド採取を採用した。方式を維持・変更する場合も、実際の取得元、精度、負荷を根拠に判断する。

## Purpose

通常Taskの実行中に採取したホストCPU・メモリ・スワップが、実際に生成したDogfood Bundleに収録され、Worker・Reviewer・検証等の実行区間と照合できる状態にする。前Taskの実装・接続・検証の不足を修正し、通常Bundleでの未確認を完了扱いした受入確認も是正する。

## External feasibility

status: not-applicable

## Contract

- 前Taskの要求と現実装を照合し、通常Taskの採取開始・実行中採取・保存・採取終了・Bundle export・analysis indexの接続を確認する。既存実装を存在だけで維持理由にせず、欠落箇所と必要な修正を一次証拠から判断する。
- Quality Gateだけを観測してTask全体の観測を満たしたことにしない。Worker・Reviewer等の通常実行中も、時刻とTask/実行区間へ結び付いた低頻度・boundedな観測を行う。Gateの実行記録がまだない通常TaskのBundleでも、取得済みのTask実行中観測を確認できるようにする。
- 既存のTask/controller/telemetry/evidenceのownerへ統合する。独立daemon、常駐service、独自Task state DB、第二の実行状態正本を追加しない。開始・終了・再入場・重複起動の責務に意味のある変更が必要なら、実装前に親Codexへ判断を返す。
- 実行中に取得済みのsampleを途中のBundle exportで読めるようにする。終了時のメモリ一括保存だけに依存し、実行中の観測がBundleから全く見えない状態を残さない。継続中・欠損・unsupported・中断・途中打切りを型付きのcoverageとして区別し、未取得をゼロにしない。
- CPU・メモリ・スワップの取得元・単位・定義・時刻基準・CPU分母・サンプリング間隔・精度を維持する。ホスト値とプロセス値を混同せず、個人環境のプロセス名やcommand lineを無条件に収録しない。
- 通常Task、Quality Gate、test-timingsとresource sampleのidentity・時刻・source bindingを整合させる。保存先とexport元が異なるcheckout、Task、state store、runtimeを参照していないか確認する。
- 修正候補と対応するruntimeを使い、通常の採取経路から通常のBundle生成までを同じ修正Task内で実証する。テストがresource JSONをfixtureへ直接注入する検証だけで代用しない。「本配置したので次回入る」という推測で検証を後続Taskへ延期しない。
- サンプリングの重複、同期初回採取の実発生回数と累積待機時間、CPU/メモリ/I/O負荷、Bundle増分を確認する。模擬Gate等のテストで実ホスト採取を反復しない既存の対策を維持し、通常作業に何十回・何千回の固定待機を加えない。採取失敗で本来のTask/Gate結果を無関係に失敗させない。
- 親Codexは最終採否前に通常のexport commandを実行し、生成されたarchiveの収録ファイルを開いて確認する。実測sampleの存在、複数時点、各domainの取得状態、identity、coverage、analysis indexの参照を確認し、採用候補/runtimeとの対応とarchive内のexact locatorを証拠にする。

## Must not

- 実ホストsampler単体の成功、fixtureへのJSON注入、unit/integration testのPASS、独立Reviewの宣言、completion commandの成功だけで通常Bundleへの接続を証明したことにしない。
- Gate限定の観測、Bundle生成時だけの単発snapshot、空のresource file、架空sampleでTask実行中の時系列観測を満たしたことにしない。
- sampleがない通常Bundleを、原因確認せず「インストール前だから」「次Taskから入る」と説明して完了扱いしない。
- 本修正を口実にCI分割、Goバージョン、既存テスト本体の意味、無関係なTask lifecycleを変更しない。
- 旧schemaの互換層、推測fallback、別監視service、独自state DBを追加しない。
- 現ACTIVEの実装へ割り込み、本Taskを勝手にACTIVEへ戻さない。実行順序は親が管理するPlanを正とする。

## Acceptance criteria

- 通常のTask実行で、Gate記録がない区間を含めCPU・メモリ・スワップの実sampleを取得し、実行中に生成した通常Bundleから複数時点の観測を読み取れる。権限不足等で実際に取得不能なdomainは、その理由とcoverageが記録される。
- 対応するGateが実行された場合、Gate run/test-timingsとresource sampleのidentity・時刻・Bundle内参照を照合できる。
- 継続中・終了・中断・欠損・unsupported・上限到達の扱いが曖昧にならず、重複collectorや無限増加、停止待ち、通常処理の無関係な失敗を起こさない。
- 採取コストとBundle増分を実測し、通常Taskでの同期待機の発生箇所・回数・累積値を示す。fixture化したテストで実ホスト採取の反復が再発していない。
- 採用候補に対応するruntimeから通常経路で生成したBundleの現物を親Codexが開き、resource file、実sample数、domain状態、coverage、identity、analysis indexを確認した証拠がある。実測用fixtureのBundleを、この証拠の代わりにしない。
- 必要なunit/integration test、既存Quality Gate、独立Review、本配置とinstalled/source一致確認が成立する。異なるOSで実行できなかった検証は未検証として明記する。
- 上記未達が残る場合はTaskを完了扱いせず、不足条件を明示する。

## Historical invariants

- 前Taskの要求正本と、本repositoryの1 Task 1 Commit、親専有metadata、source binding、失敗時の正確なevidence、machine-only data原則を維持する。
- 実装・検証・独立Reviewは既存のGLM lifecycleへ委譲し、親Codexが実差分と通常Bundleの現物から最終採否を判断する。
- 通常completionの履歴をIMPLEMENTATION_HISTORY.mdへ複製しない。

## Dependencies

none
