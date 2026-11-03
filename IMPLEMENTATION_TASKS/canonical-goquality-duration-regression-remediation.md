# Task: 正規Go品質検証コマンドの実行時間退行を修正

## Original instruction

2026-11-04 ユーザー指示：Docker移行後に一度大幅短縮した実行コマンドが約2倍まで長時間化している。原因を調査し、実装を修正する。

## Amendments

none

## Resolved references

- [Audit #1399](https://github.com/shinderuman/codex-worker-orchestrator/issues/1399)：Docker Linux ARM64 / Go1.25.4移行直後、正規`go-test` **120.400秒**（Docker内部98.065秒）、`go-test-race` **331.963秒**（同309.133秒）、Git `6ec66e3aa9a3341f25fde46804efe8f789eeafe4`。
- [Audit #1500](https://github.com/shinderuman/codex-worker-orchestrator/issues/1500)：Git `d8d1b203907a5e9c3921b1a6a3aa0985230c68bf`、通常 **278.669秒**、race **705.266秒**、合計 **2.18倍**に増加。raceの`internal/app` **652.643秒**。
- [Audit #1501](https://github.com/shinderuman/codex-worker-orchestrator/issues/1501)：Git `c8acd6190f46c8cc281b74d901cd91601d0e95bb`、通常 **274.555秒**、Docker内部230.563秒、`internal/app` **221.491秒**、`internal/controller` **118.090秒**。このTaskではrace未実行のため直近のraceは#1500の値。
- `codex-efficiency-next-checkpoint.md`と`publication-sequence-roundtrip-evaluation.md`はread-only評価Taskであり、性能退行の実装修正を所有していない。

## Purpose

品質を維持しながら、正規Go検証コマンド自体の実処理時間を短縮する。実行時間が移行直後の2倍以上へ戻っている状態を解消する。

## External feasibility

status: not-applicable

## Contract

- 正規`go-test`と`go-test-race`について、Go版、Linux ARM64 Docker、資源、cache状態、対象testを対応付けて実測し、過去と現在の差をpackage・test・subtest・fixture初期化・準備の粒度で分解する。外部待機やrate-limitを実処理に混ぜない。
- 最長の`internal/app`と`internal/controller`を優先し、重複した初期化・process/Git操作、追加test、fixture構築、シリアルな処理など、実際の時間増加要因を特定する。テスト数・条件差で説明できる部分と改善可能な部分を分ける。
- 必要なproduction/test/Harnessの実装を**修正**し、正規コマンドの経過時間を実測で大幅短縮する。適切なpackage・test構造の分割は可。ただし二重ownerや不要な独自framework・無関係な変更を増やさない。
- test・raceの全シナリオ、障害検出力、実行条件を維持する。単純なテスト削除、重要な検証のskip、キャッシュによる見かけだけの成功、品質Guardの緩和で達成しない。

## Acceptance criteria

- 同条件のbefore/afterで通常・race双方の正規コマンドwall、Docker内部、主要package/test時間、増減の原因を実測する。Docker移行直後の120.400秒／331.963秒を比較基準とし、残差があるなら説明する。
- 主要ボトルネックを実装で修正し、通常・race双方の実行時間が短縮している。test名・個数・重要シナリオ・正負の振る舞いを比較し、検証対象が欠落していない。
- **調査・計測・記録のみでは未完成**。長時間化の主因が未修正のままなら完成扱いせず、根拠と残作業を明示する。
- Task差分をレビューし、無関係な修正を含めない。正規Go品質検証・Lint・Install Smokeを維持する。

## Historical invariants

- 正しさと品質を維持しつつ、毎Taskの長時間検証コマンドを短縮する。既存の評価Taskへ計測値を記録しただけで問題を解消したと扱わない。

## Dependencies

none
