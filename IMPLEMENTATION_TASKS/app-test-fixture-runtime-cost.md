# Task: appの完了・再入テストのfixtureとprocess反復時間を削減する

## Original instruction

ユーザー原文（2026-11-03、順序どおり）:

```text
今回internal/app の肥大化がまた見受けられるのでinternal/appの肥大化、ほかも肥大化してないかも合わせてチェックするように
```

```text
Appの肥大化というのは主にテストの時間に対して言っている
```

親Codexの中間評価による採用要求:

```text
既存のfull Go/raceログでappの287.676秒／722.579秒がgate終了の主な制約となっている。完了・parent-only完了・再入の上位3testに調査を絞り、fixture setup、Git、実CLI subprocess、race追加時間の内訳と反復を測定し、品質を維持できる同一責務の改善を実装する。遅いという理由でscenarioを削らず、test selection/retirementやrace検出の弱化へ拡張しない。根本原因未測定のまま並列化・cache・package移動を先に決めない。
```

## Amendments

### 2026-11-03 CI平準化の追加

原文:

```text
独立Task化するなら
https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41600207557
CIを平準化することも目標の一つとしろ
```

### 2026-11-03 Appテストと他テストの実行時間平準化

ユーザー原文:

```text
Appのテストが10分とかかかるならとてもじゃないけど完成じゃないだろ
Appのテストと他のテストが同じぐらいで終わるようにしろ
これはテストを分割するでもいいしAppパッケージを分けるでもいいしWorkflowのtest1, test2 見たいのを増やすでもいい
テストを削って達成というのは基本的にはなしだ
ただし重複したテストがあるならそれは削っていい
```

### 2026-11-03 AppジョブをMatrix:basicより短縮

ユーザー原文:

```text
Matrix:basicの一番遅いものよりAppのほうが早くなるように調整しろ
```

### 2026-11-03 Workflowの保守性

ユーザー原文:

```text
Workflowファイルの実装がだいぶひどいことになっているが関数化みたいなことはできないのか
リファクタリングできるならしてほしい
保守性を保ちたい
できないならやらなくてもいいが
```

## Resolved references

- 採否と一次run locatorは`review-evidence/codex-efficiency-followup-2026-11-03.md`。実装開始時はcurrent sourceで再確認し、既存観測を現sourceの測定値へ付け替えない。
- 初期対象は`TestTypedParentCompletionWorkflowThroughProductionBoundary`、`TestParentOnlyTaskCompletionThroughProductionBoundary`、`TestParentOnlyReentryThroughProductionBoundary`と共有fixture/helper。
- `104-test-impact-selection.md`はtest selection/retirementの許可待ち正本であり、本Taskはその許可条件を変更しない。

## Purpose

app full/race gateの長時間化を、保証するbehaviorを失わず、反復fixture・process境界の実コストから改善し、CIジョブの時間を平準化する。

## External feasibility

status: not-applicable

## Contract

- 上位3testとhelperについて、scenario、保証するinvariant、fixture生成、Git操作、process起動・待機、snapshot/evidence処理の時間・回数をboundedに測定する。通常とraceを分け、setupとassert対象のproduction経路を区別する。
- raceの増分原因を測定する。既存shimはtest executableを再起動するため、根拠なく毎回のgo buildが原因としない。測定前にGORACE、timeout、環境、concurrencyを変えて改善値を作らない。
- 同一source/toolchain/実行条件を明示してbefore/afterを比較する。testとsubtestのelapsedを二重加算せず、package elapsedとgate全体時間を分ける。単回差や非同条件は限界を明記する。
- 指定CI run `41600207557`と実装開始時のcurrent workflowを確認し、test job/shardの所要時間・待機・配置を測定する。CIジョブ所要時間の平準化とcritical path短縮も目標とし、app内の短縮だけでなく既存のCI分割・配分の改善を評価する。aggregate gate、必要なscenario/race/独立Review、実sourceへの検証帰属を維持する。
- 同一責務で有効なsetup再利用・不要反復の削減を実装する。状態・Task/source・remote・attemptの分離を保ち、実境界を要する正常/中断/復旧caseは正規入口を通す。
- owner単体とCLI接続へ検証を分ける場合はscenario/invariant対応と依存方向を示し、意味のある設計変更を実装前の親判断へ返す。単なるtest file/package移動を時間改善としない。
- 改善案がcoverageを狭める、追加管理機構が割に合わない、または有効な削減点がない場合は、具体的な測定と代案を親へ返し、無理な実装を行わない。

## Must not

- test impact selection、scenario retirement、test skip、必要なrace/独立Reviewの省略を行わない。
- race reportの抑制、process終了・競合検出を弱める変更、sleep経過による成功判定を行わない。
- production admission、Git/source binding、staging、Publication、復旧条件をfixtureの都合で変更しない。
- テスト間の可変State/remote共有、独自cache/daemon/第二state machineを追加しない。
- 全repository再監査、controller/stateの独立最適化、Codex削減率への時間換算を行わない。

## Acceptance criteria

- 上位testの時間内訳・起動回数・保証するbehaviorを示し、支配原因と不明点を区別できる。
- 改善前後で同等のscenario/invariant coverageと、実CLIを通す必要のある境界を確認できる。
- 通常/raceのfocused測定と必要な正規gate・独立Reviewがsourceへ束縛され、app/package/gate時間を区別して効果を説明できる。
- 同一CI runのMatrix:basicに含まれる最長ジョブより、Appの各通常/raceジョブを短時間で完了させる。AppのCIテストを他のテストジョブと同程度の所要時間へ平準化し、job/shard別時間と全体critical path、実行量・反復・coverageを比較して効果と限界を示す。テスト選別による省略は行わず、重複削減は検証の同等性を示す場合に限る。単に最長jobを細分化して重複setupや総コストを増やした結果を無条件に改善としない。
- 改善未成立時は親判断を求め、未検証の時間削減を完成としない。

## Historical invariants

- 品質維持とCodex消費削減を同時に評価し、machine wall-clockをtoken削減率へ外挿しない。
- 正常完了、失敗後再入、parent-only authority、source-bound evidenceと復旧の保証を維持する。

## Dependencies

none
