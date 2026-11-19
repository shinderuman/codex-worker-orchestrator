# Task: リポジトリに残っている後方互換性を棚卸しして削除する

## Original instruction

リポジトリに残っている後方互換性を棚卸しして削除する

## Amendments

2026-11-20

> あとそのタスクで今後同様の後方互換性が作られない仕組みを追加しろ
> 既存もすでにいくつかそういう仕組がある

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
