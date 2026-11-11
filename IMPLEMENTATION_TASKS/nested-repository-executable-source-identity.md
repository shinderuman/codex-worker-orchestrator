# Task: nested repository の実行権限を source identity に含める

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534907052

**[P2] ネストした未追跡リポジトリ内の実行権限変更が source digest に反映されません。** `NestedUntrackedRepositoryEntries` は `d.Type()`（ファイル種別ビットのみ）を `Mode` に保存し、`hashUntrackedEntry` は通常ファイルについて content SHA-256 だけを hash に加えます。したがって `nested/tool.sh` の chmod `0644 → 0755` は `CaptureGitSnapshot(...).WorktreeDigest` を変化させません。一方 `dockertest/source.go` の `archiveNestedRepository` → `archiveEntry` → `tar.FileInfoHeader(info,...)` は実際の permission bit を Docker 転送アーカイブに保持します。前後の digest が同じでもコンテナで実行可能かどうかが変わり、`unchangedSource` / validation の source-equivalence が異なる実行入力を同一と誤認します。DirEntry.Type ではなく `Info().Mode()` などで実ファイルモードを捕捉し、hash に少なくとも executable bit を含めてください。実験でも同一ファイルの `DirEntry.Type()` は 0644/0755 とも通常ファイルを返す一方、tar header は異なる mode を保持しました。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534907052) — `glm-worker/internal/state/snapshot.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

DirEntry.Typeとcontentだけでは転送されるexecutable modeの差を失う。

## External feasibility

status: not-applicable

## Contract

- nested未追跡repositoryの通常fileで実modeを捕捉し、実行に影響するexecutable bitをsource digestへ束縛する。archiveとsource-equivalenceの意味を揃える。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 内容不変の0644→0755と逆変更でWorktreeDigestが変わり、Docker転送modeと対応する。symlink/特殊fileの拒否と既存source変化検出を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
