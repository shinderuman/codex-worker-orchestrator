# Task: Resume checkpoint の厳格な現行 schema 検証

## Original instruction

[#1510 の追加項目 #1547](https://github.com/shinderuman/codex-worker-orchestrator/pull/1547) として、resume checkpoint の旧フィールド専用拒否処理を撤去し、現行 schema の厳格な検証に統一する。

## External feasibility

status: not-applicable

## Contract

- Resume checkpoint の version と明示必須キーを既存条件で検証し、その後、現行 schema の共通 decoder で未知フィールドを拒否する。
- 旧フィールド名の個別列挙を廃止する。#1521 で明文化した廃止済み `risk_floor_reemit` も、未知フィールドとしてモデル呼出・状態変更前に拒否する。
- 現行の通常再開・停止種別・補正済み Reviewer 保持の意味と source binding を維持する。

## Must not

- 旧 schema、旧フィールド、欠落値の補完、移行、再送処理、互換 fallback を導入しない。
- Resume checkpoint 以外の decoder や状態契約を変更しない。

## Acceptance criteria

- 現行 version の有効 checkpoint は読み込め、従来の必須キー欠落、旧 version、未知フィールド、廃止済み3フィールドは拒否される。
- #1521 の補正と再入が通常入口から動作する。未知フィールドを含む状態からモデル呼出や状態更新へ進まない。
- 関連通常・race、Lint、独立レビュー、正規候補の検証を通す。

## Dependencies

none
