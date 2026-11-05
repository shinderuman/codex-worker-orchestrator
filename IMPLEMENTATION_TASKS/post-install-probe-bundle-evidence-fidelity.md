# Task: post-install smokeのBundle証拠を実際の出力に一致させる

## Original instruction

2026-11-05のDogfood Audit（#1506）で、正規post-install smokeが成功しているにもかかわらず、Bundleに実際のprobe logが入らず、存在しない通常Go gate用ファイルが欠測として記録される問題を確認した。

## Amendments

none

## Resolved references

- [Audit #1428](https://github.com/shinderuman/codex-worker-orchestrator/issues/1428)でも2つのpost-install smoke runについて同じ`gate.log`/`resource-samples.json`各2件の欠測を記録している。初回観測ではなく、以前から再発していたform別収集漏れである。
- [Audit #1506](https://github.com/shinderuman/codex-worker-orchestrator/issues/1506)、Bundle `6a3c98f2-e67d-4868-80c0-14361b08b054`。post-install smoke run `72148a799c2d6c6de32e7974ffa01c4c` は`form=post-install-smoke`、実行結果PASS、5.352秒、`RunRecord.Log`は`probe.log`。
- 現行`glm-worker/internal/controller/evidence_export_validation.go:collectGateRun`はformを区別せず`gate.log`と`resource-samples.json`を収集対象とする。今回のmanifestにはこの2件が`missing:true`で記録され、実`probe.log`はZIPに収録されない。
- 通常Go test/raceの`gate.log`・`resource-samples.json`・`test-timings.json`の収集は正規に動作しており、変更してはならない。

## Purpose

Bundleの検証証拠を実行formと真の出力に一致させ、存在しないartifactの誤欠測と実際のprobe logの欠落を解消する。

## External feasibility

status: not-applicable

## Contract

- post-install probeを通常Go品質Gateと区別し、実行済みRunRecordのsource・log locator・identity・formから、**実際のprobe出力**を既存Bundle収集機構で保持する。
- Go専用の資源サンプラーや`gate.log`を、存在しないpost-install probeへ機械的に要求しない。ただし本来存在する証拠の欠落・読み取り失敗は従来どおり欠測と記録し、PASS扱いで隠さない。
- 任意の外部パスを信用して読み込まない。Task/session/Run IDの正当な保存領域とsource bindingを確認し、path改変・越境・symlink・競合時はfail-closedにする。
- 通常Go test/race、Install Smoke、post-install smokeの現行実行と、既存のBundle/analysis-indexの正しいsource帰属を維持する。新規第二Collectorや汎用frameworkを作らない。

## Must not

- probeをGo品質Gateへ偽装し、存在しない資源サンプルを生成して見かけのcoverageを増やす。
- RunRecordの成功状態・検証要求・必要なログ収集を緩和する。
- 過去Bundleの欠測を、実証のない遡及的な成功記録に置き換える。

## Acceptance criteria

- 実際のpost-install smoke実行→Bundle exportで、正規`probe.log`が読める形で収録され、manifestのsize/SHA-256とrun provenanceが一致する。今回のような存在しない`gate.log`/resourcesの誤欠測が出ない。
- run recordのsource/Task不整合、probe logの欠落/改ざん、範囲外のパス、競合を負例として検証し、不正な証拠を完全と扱わない。
- 通常Go test/raceとInstall Smokeも現行どおりexportされ、既存のsource-bound Gate、test timing、resource samplingの証拠が失われない。
- Bundleの正負実行、manifest完整性、差分と実行時間の検証で修正を確認する。

## Historical invariants

- Task完了の品質基準を維持し、収集の正確さと完全性を分けて報告する。

## Dependencies

none
