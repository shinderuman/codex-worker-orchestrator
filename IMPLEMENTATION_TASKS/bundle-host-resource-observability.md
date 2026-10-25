# Task: Dogfood BundleにホストCPU・メモリ・スワップの実行時観測を収録する

## Original instruction

````text
Bundleにスワップ量やCPU使用率を記録したほうがいいな
それをCodexに作業させるIMPLEMENTATION_PLANを追加するIssueとPRも作っておいてくれ
````

## Amendments

none

## Resolved references

- 「Bundle」: 現行のCodex + GLM Dogfood Bundle。通常Taskの実行・検証時に生成される証拠一式であり、現行のbundle/export経路を実装時に確認する。
- 契機: M1 Mac mini (16GB)のローカル通常Goテストは25～32分かかる一方、Linux CIでは分割実行で約4分。直近Bundleの測定ではMac上の `internal/app` が約30分、`internal/controller` が15～17分。Goバージョン、OS、テスト並列化および環境資源の差を識別する必要がある。
- 主な既存証拠境界: `glm-worker/internal/controller/evidence_export.go` と、`glm-worker/internal/qualitygate/test_timings.go` / `glm-worker/internal/app/quality_gate_execution.go`。実際のCollectorとownerは実装開始時のソースを正とする。

## Purpose

Dogfood Bundleから、ローカル/CIでテスト・Worker・Reviewer等の実行が遅くなったときに、CPU使用率、メモリ使用量・圧迫、スワップ使用量の時間変化を、実際の実行区間と対応付けて検証できるようにする。

## Contract

- 現行の実行・Quality Gate・Bundle export経路を確認し、既存のtelemetry / evidenceのownerへ統合する。別個の監視daemon、常駐service、独自のTask state DBは増設しない。
- 起動時の一回だけでなく、Taskおよび高コストな検証の実行中に低頻度・boundedにリソースをサンプリングし、時刻と実行区間（少なくとも検証run identity）を結び付けてBundleに収録する。収集開始/終了・部分期間のcoverageも表現する。
- 少なくともホスト全体のCPU使用率、使用可能/使用中メモリ、スワップ使用量（可能なら増減、使用率や圧迫の判別に必要な値）を対象とする。必要ならGo testプロセスまたはprocess treeに帰属可能なCPU/RSS等を追加し、ホスト全体の値と混同しない。
- macOS（Apple Silicon）とLinuxで取得元・単位・定義・観測精度を明示し、取得不可能・権限不足・途中停止は型付きのmissing/unsupported/partialとして保持する。未計測をゼロ使用量とみなさない。
- CPU使用率の分母（全論理CPU対コア換算など）、サンプリング間隔、時刻基準、単位、ホストとプロセスの区別を明文化する。Bundleによる分析者がMac/CIの実測を誤比較しないようにする。
- 計測・収録処理自体がCPU/メモリ/I/Oを著しく消費せず、無限増加・重複収集や停止待ちを起こさないこと。取得失敗で本来のTask/Quality Gateを無関係に失敗させない。
- Bundle export後も収録したデータが読め、test-timingsや実行時刻と突き合わせられる。個人環境のプロセス名・command line・機密情報を無条件に収録しない。

## Must not

- 今回の観測追加を口実にCI分割方式、Goバージョン、実テストのロジックやTaskライフサイクルを変更しない。
- 単一の最終スナップショットだけで「実行中の資源不足を把握できる」と扱わない。
- すべてのOSで同じ指標を取得できると仮定した擬似値・ゼロ埋めをしない。
- 外部監視serviceや別Task専用の蓄積形式を標準Workflowへ追加しない。
- 既存のスキーマ互換層を新設しない。必要なschema更新を現行正規ownerに集約する。

## Acceptance criteria

- macOS / Linuxの取得可能なresource sampleが時刻・単位・source・実行区間とともにBundleへ収録され、CPU・メモリ・スワップの時間推移を再構成できる。
- sampleの欠損・unsupported・途中打切りの証拠が曖昧にならない。
- resource sampleとQuality Gate run / test-timingsを時系列で照合できる。
- サンプリングの追加によるwall-clock / CPU overheadとBundle増分を実測し、既存Workflowの検証速度を大きく悪化させない。
- unit / integration test、Bundle schema/evidenceの確認、独立Review、必要な既存のQuality GateがPASSする。異なるOSで実行できなかった項目は未検証と明記する。

## Historical invariants

- `IMPLEMENTATION_RULES.md`の1 Task 1 Commit、Task authority、失敗時の正確なevidence、machine-only data原則を維持する。
- Parent Codexは最終採否、実装/検証はGLMへ委譲する正規Lifecycleを維持する。

## Dependencies

none
