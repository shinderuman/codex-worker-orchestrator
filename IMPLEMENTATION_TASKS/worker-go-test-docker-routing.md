# Task: GLMのGoテストにDocker使用を強制する

## Original instruction

2026-10-27 ユーザー原文:

````text
そう言えば今回Dockerを使うようにしたわけだがGLMがDockerを使うように強制できているんだろうか
GLMがDockerの存在に気づかなかったりとりあえずDocker使わないで動けばいいやみたいな判断されたら今回の施策が無駄になってしまう
````

2026-10-27 ユーザー原文:

````text
GLMのトークン消費を見ると待機がやたら多いように感じる
それはMac上でテストをやって待ち時間が長いのだろうか
だとしたらもうだいぶ改善しないといけない証左だと思う
Codexに介入して作業効率をあげたほうがいいだろうか
そもそも介入したところで作業効率が上がるのかもわからねえが

GLMの進め方をまだCodexがレビューしたわけじゃないからGLMの作業内容自体を単純に問題ではないと思う
````

2026-10-27 ユーザー原文:

````text
このGLMがMac側でテストをやってしまう問題はいつ直す予定なんだ？
````

2026-10-27 ユーザー原文:

````text
これは先頭で直すタスクだろ
PR作れ
````

2026-10-27 ユーザー原文:

````text
GLMに判断をさせるのが間違ってる
GLMは絶対的に保守的に判断する
つまりDockerよりMac環境を保守的に絶対に使う
なので絶対にDockerを強制させるようにしないとダメだ
````

2026-10-27 ユーザー原文:

````text
いや方針自体はCodexが判断するようにしろ
方針、設計、はCodexがやる
要件だけ記載しろ
````

## Amendments

## Resolved references

- 「今回Dockerを使うようにした」は本repositoryで導入したMacの正規Go full/race検証用Docker backendを指す。
- 「GLMがMac側でテスト」はWorkerの作業中に行う局所Goテストを指す。正規Quality GateのDocker化とは異なり、現行の局所テストにはMacネイティブ実行経路が残っている。
- 2026-10-27のDogfood Bundle `62b91ba5-aea3-44f0-9a9c-0f126f057e69.zip`で、GLMによるMac上の長時間Goテストとsandbox制約に起因する失敗が確認された。具体的な原因と改善対象はCodexが一次証拠と現在の実装から確定する。
- 「先頭」は現行ACTIVEを変更せず、`IMPLEMENTATION_PLAN.local.md`のNEXT先頭を意味する。

## Purpose

GLMによるMac上のGoテスト実行を防ぎ、Docker利用をGLMの選択や認識に依存させず、長時間待機と無駄な検証を解消する。

## External feasibility

status: not-applicable

## Contract

- Apple Silicon Mac上でGLMが実行するGoテストについて、Dockerでの実行を機械的に強制する。GLMにMac nativeかDockerかの選択を委ねない。
- Docker使用が成立しない場合でも、Mac nativeへの暗黙の代替実行やテスト省略による成功扱いを認めない。
- 必要なGoテストの検証範囲・正確性と既存の正規Quality Gateを維持し、改善前に発生した長時間待機と不成立テストの問題を解消する。
- 実現方針、設計、適切な責務分担、必要な変更範囲は、Codexが現行実装・証拠を確認して判断する。

## Must not

- GLMへの自然言語の指示や自主判断だけにDocker使用を依存させない。
- Docker強制を理由に必要な検証を省略したり、Mac native実行を暗黙に許容したりしない。
- このTaskの起票だけで現行ACTIVEを切り替えない。Web GPTが設計・実装方式をTask本文で決めない。

## Acceptance criteria

- 実際のGLM実行経路で、GLMの選択によってMac nativeのGoテストが実行されず、Docker使用が機械的に強制されることを検証する。
- Dockerが使えない等の失敗条件でも、Mac nativeへ迂回せず失敗・未検証が正しく扱われることを検証する。
- 必要な検証の網羅性と正規Workflowを維持し、従来のMac側での長時間待機・不成立テストの再発防止と作業時間への効果を証拠で確認する。

## Historical invariants

- Git/Plan/Task authority、正規Quality Gate、必要な独立Review、1 Task 1 Commit、品質保証を維持する。

## Dependencies

none
