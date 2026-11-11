# Task: staging request の一意identityと非置換publish

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534920902

**[P2] 並列するstaging requestが衝突すると、別テスト要求を上書きして同一結果を双方へ返せます。** `StagingClient.token()` は `time.Now().UnixNano()` のみを使い、`writeRequest` は最終pathの `Lstat` と固定名 `.request-<token>.tmp` への `os.WriteFile` と `os.Rename` を非原子的に実行します。2つの `goquality --staging` プロセスが同じtickを得て両方とも `Lstat` で不存在を観測すると、両呼び出しが成功を返す一方、最終requestは片方だけになります。双方が同じ `response-<token>.json` を読み取るため、例えば別のpackage引数のテスト結果を自身の成功として扱えます。固定token・2並列書き込みの最小再現で両者 `nil` 成功／requestファイル1個を確認しました。プロセス単位で衝突しない乱数tokenを生成し、tempにも専有作成を用い、最終publishも既存pathを置換しない方法へ変更してください。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534920902) — `glm-worker/internal/dockertest/staging.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

時刻tokenと非原子的Lstat/固定temp/renameが別requestの結果混同を許す。

## External feasibility

status: not-applicable

## Contract

- 既存protocolのowner/clientで衝突耐性のあるtoken、専有temp、既存最終requestを置換しないpublishを採用する。新token形式が必要なら双方を同時更新し旧形式の恒久bridgeは作らない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 同時刻の異なるpackage要求が別identity/responseへ結び付き、強制token衝突では一方を拒否して上書きしない。temp競合・token再利用・cleanupを検証し、他要求の成功を流用できない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
