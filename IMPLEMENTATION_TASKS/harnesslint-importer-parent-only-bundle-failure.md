# Task: harnesslintのimporter Panicとparent-only Bundle生成失敗を解消する

## Original instruction

ユーザー原文（2026-11-03、順序どおり）:

```text
そのPanicの解決もタスクにしないといけないんじゃないの
```

```text
あとそのPanicがBundle生成の妨げになってるものなら同じタスクで修正しろ
```

```text
いまparent onlyだとBundleが生成できないバグが有る
おそらく原因は一緒だ
```

親Codexの採用要求:

```text
配置済みharnesslintをPATHから呼び出した際のGo importer Panicを調査・修正する。parent-onlyでBundle生成できないというユーザー報告も同じTaskの調査対象とし、共通原因かを一次証拠で判定する。同じPanicがBundle生成を妨げている場合は同Taskで修正し、通常Taskとparent-onlyの正規Bundle生成・完了接続を検証する。別原因の場合はその根拠と必要な修正範囲を親へ返し、原因を推定で統合しない。
```

## Amendments

none

## Resolved references

- 今回のPanicはrepository rootでPATH上の`harnesslint`を実行した際の`panic: cannot decode "crypto/sha256", export data version 4 is greater than maximum supported version 2`。stackは`go/internal/gcimporter.Import`、`qualityWiringPartialBindings`、`qualityWiringRequiredGoTokens`を通る。
- 実行時の観測は`go version go1.27.0 darwin/arm64`、GOROOT `/opt/homebrew/opt/go/libexec`、配置済み`/Users/shinderumanm/.local/bin/harnesslint`のbuild version `go1.25.4`。配置済みbinaryのsource一致とPanicの根本原因は未確認であり、build/runtimeの不一致だけで原因を確定しない。
- 同じ評価中、repositoryの`./harnesslint`は`quality-tools.yml`のGo versionを選択する既存wrapperとしてexit 0、pass/fixed 0/violations 0だった。記録は`/private/tmp/codex-efficiency-followup-2026-11-03-lint.json`。wrapper成功を配置済みbinaryまたはparent-only Bundle成功へ付け替えない。
- 前回のGo importer Panic記録は`review-evidence/codex-efficiency-webgpt-adoption-2026-11-03.md`。今回評価と追加要求は`review-evidence/codex-efficiency-followup-2026-11-03.md`。

## Purpose

Lintのtoolchain/importer失敗をPanicとして流出させず、parent-onlyの正規Bundle生成を妨げる原因を解消する。

## External feasibility

status: not-applicable

## Contract

- source・配置済みbinary・Go/GOROOT/toolchain・canonical wrapper・GLM quality処理・Bundle生成が実際に通るcommand/ownerを照合し、Panicの最小再現と成立条件を固定する。
- parent-onlyのBundle生成失敗について、実際の正規操作、source/attempt/candidate、最初の失敗箇所、error/stackを確認する。ユーザーの共通原因仮説を検証し、未確認ならunknownに保つ。正常系のwrapper lintだけで解消扱いしない。
- 共通原因なら同Task内でLintとparent-only Bundleの両経路を修正する。別原因・独立責務なら一次証拠と具体的修正案を実装前に親へ返し、黙って片方を未達のまま完了しない。
- 既存のtoolchain選択・importer・quality command ownerへ集約し、配置済み入口とproduction wiringの不一致を直す。単に親が毎回別commandや環境を手設定する運用へ逃がさない。
- 必要な型/構文/品質検証を維持する。成立しないtoolchainや入力は既存structured errorでfail closedとし、blind recoverで検査済み・PASSへ変えない。
- 通常Taskとparent-onlyそれぞれのBundle生成、source-bound validation、既存Publication/Retireへの接続を確認する。失敗時の既存candidate/journalを保ち、State補正・偽のvalidation・手動Bundle生成で正規復旧を代替しない。

## Must not

- 「原因は一緒」という仮説を証拠なしに根本原因としない。
- parent-onlyを通常GLM Taskへ降格し、追加model callで回避しない。
- Lint/Bundle gate、authority、source binding、必要validationを省略・緩和しない。
- 全体Panic catch、独自toolchain管理層、旧schema/command互換、別Bundle生成経路を追加しない。
- GLM worker/reviewerにcommit/push等のGit remote writeを許可しない。

## Acceptance criteria

- Panicとparent-only Bundle失敗の再現・原因・共通性/差異をsourceと実行記録で説明できる。
- 正しい実行条件で配置済み/productionの正規LintがPanicを起こさず、異常条件もstructured failureとして観測できる。
- 通常Taskとparent-onlyの正規Bundle生成・完了接続が、修正後sourceと必要な本配置に束縛された証拠で確認できる。
- 独立Reviewと必要gateを通し、検証未達を明示してから親Codexが最終採否を判断する。

## Historical invariants

- parent-onlyでは追加GLM呼出を行わず、canonical authority・品質検証・親の最終採否を維持する。
- machine失敗を成功に読み替えず、必要な検査とsource-bound evidenceを保持する。

## Dependencies

none
