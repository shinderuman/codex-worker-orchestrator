# Task: installer missing dependency Homebrew hint

## Original instruction

```text
パッケージがないときにBrewコマンドを表示するよう修正してPRだして
```

## Amendments

```text
[2026-08-27]
こっちさきやれ
```

## Resolved references

- `こっち`は、PR #5 merge後の`./install.sh`が`plan final head: HEADのplanに現在のGit境界branchがありません`で停止したmetadata不整合の修復を先行する指示を指す。
- metadata不整合はPR #6 `2c8bf5fe22b529dd446c65bd43b6a2289819730d`でmainへ修復済み。

## Purpose

installerがHomebrewで導入できる必須commandの欠落を検出した際、失敗理由だけでなくその場で実行できるHomebrew install commandも表示する。

## Contract

- 既存のfail-closed dependency checkとnon-zero終了を維持する。
- Homebrew formulaを持つ必須commandが欠落した場合、stderrへ`brew install <formula>`を含む案内を追加する。
- commandを自動installしない。
- Homebrew formulaを持たないOS標準commandの欠落では誤ったformulaを案内しない。

## Must not

- dependency checkをoptional化しない。
- `brew install`をinstallerから実行しない。
- quality gateの依存関係やruntime behaviorを変更しない。

## Acceptance criteria

- `shellcheck`欠落時に従来の`required command not found: shellcheck`と`brew install shellcheck`が両方stderrへ出る。
- 対応する回帰testを追加する。
- 通常install smokeの既存挙動を維持する。

## Historical invariants

- installerはrepository test suite/lint自体を実行せず、runtimeに必要なcommandの存在をfail closedで確認する。

## Dependencies

none

## Review findings

none

## Current boundary

PR #6によるmetadata修復はmainへmerge済み。`gpt/installer-brew-hint-v2`でproduction変更とinstall smoke regressionを作成し、main向けPR・merge後のlocal runtime acceptance待ち。
