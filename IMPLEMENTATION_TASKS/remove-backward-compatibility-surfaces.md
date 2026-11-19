# Task: リポジトリに残っている後方互換性を棚卸しして削除する

## Original instruction

リポジトリに残っている後方互換性を棚卸しして削除する

## Amendments

2026-11-20

> あとそのタスクで今後同様の後方互換性が作られない仕組みを追加しろ
> 既存もすでにいくつかそういう仕組がある

## Resolved references

起票時に確認済みの後方互換処理

- `glm-worker/internal/codexcontext/context.go` の `retiredManagedConfigSHA256`、`isRetiredManagedConfig`、`canRetireManagedConfig` は、廃止済みの生成設定を固定digestで識別し、専用の拒否・削除へ送る。#1672で追加されたこの分岐を削除する。
- `glm-worker/internal/bundleanalysis/review_gap.go` の `reviewGapCauseStatus` は、原因が空のCodexレビューにだけ `cause_missing_legacy` を返す。`glm-worker/internal/state/parent_review.go` の専用状態と、それを期待する既存テストを対象に含める。原因が欠損した現行の入力は現行の要求に沿って検証・分類する。

実装時に現行契約と照合する箇所

- `glm-worker/internal/state/forward_schema.go` の `ModelCallLog.UnmarshalJSON` は版またはschema revisionが現行値と一致しないとき、versionだけを持つ値へ置き換えて成功を返す。`glm-worker/internal/state/telemetry_corpus.go` は同じ不一致をdecode前に扱う。呼出元を照合し、専用の成功値を残さず現行の検証と汎用の除外で扱う。
- `codex/instructions/glm-execution.md` は `--decision-stdin` と `--fix-stdin` をrecovery/debug互換として説明する。`glm-worker/internal/app/command.go` に受付があり、`glm-worker/internal/parentaction/action.go` の現行staged actionも両方をWorkerModeに使う。直接互換入口と現行内部通信を照合し、必要な認可と通信を保ったうえで互換入口を削除する。

## Purpose

現行の実装規則に従って、以前の利用方法を維持するために残っている受付経路と識別分岐を削除する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

production code、設定、起動スクリプトと参照する規則を対象に、以前のCLI、設定、ファイル名、入力形式、状態、schema、actionを受け付ける互換経路と、廃止済み入力を識別して専用処理へ送る分岐を棚卸しする。codex/instructions/worker/common-code.mdの後方互換禁止を適用する。

#1672の旧 .codex/config.toml をdigestで識別する拒否・削除の専用分岐を対象に含める。リポジトリ固有指示の配置分離とユーザー所有設定の保持を維持する。互換用として文書化されている直接stdin入口は、現行のstaged actionから呼ばれる内部通信と照合する。版や項目の欠損を特別扱いする既存処理も現行仕様に照らして判断する。

既存のcutover検証、CLI登録、schema検証と#1678で変更した検証を確認し、既存の正規ゲートで同種の互換経路の再導入を検出する。

## Must not

廃止する経路に対するalias、専用の拒否、変換、移行、フォールバックを残さない。現行の正規状態遷移、権限、証拠、品質ゲート、ユーザー所有ファイルを維持する。棚卸しやレビューの証跡を専用ファイルとしてコミットしない。

## Acceptance criteria

旧入力は現行の汎用検証で拒否またはスキップされる。現在必要な呼び出しは正規の入口から到達でき、Task・source・権限の束縛を維持する。互換経路を生産コードの登録と実行経路から削除し、正規検証を通す。確認した残存箇所と削除結果は報告に記載する。 既存の検証を実行し、削除した側の経路が戻る代表例で追加した再発防止が失敗することを確認する。現行の入口・schemaを使う代表例は通過する。

## Historical invariants

現行形式と正規の判定元を使う。1 Task 1 Commitで、最終検証とController Publicationを通す。

## Dependencies

none
