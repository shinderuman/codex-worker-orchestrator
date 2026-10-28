# Task: 一時実行ファイルのETXTBSYによる非決定的失敗を解消する

## Original instruction

2026-10-29 ユーザー指示（原文）:

```text
この最後の方にテストがコケてるやつってさっき起票してるやつと一緒じゃねえか？
一緒だとしたらさっきのIssueを修正してPRを作れ
修正はIMPLEMENTATION_PLANの起票だ
```

## Amendments

2026-10-29 ユーザー追加指示（原文）:

```text
internal/app, internal/runner だけの修正じゃなくて同じ問題が発生しそうな箇所を全部検出して作業するタスクにしないといけないんじゃないの
```

## Resolved references

- 既知の発生例: `glm-worker/internal/app/controller_completion_install_test.go` の `TestInstalledCompletionRuntimeRequiresExactCleanSource/same`、`glm-worker/internal/runner/git_authority_temp_root_test.go` の `TestGitAuthorityClaudeWrapperOwnsProcessTempRoot`。前者はテスト生成物、後者はproductionの `prepareClaudeWrapper`（`git_authority_claude_wrapper.go`）による生成物の起動時に `fork/exec ...: text file busy` (Linux `ETXTBSY`) が発生した。
- **この2件は検索と再現の入口であり、対象の上限ではない。** 両者が同一root causeか、FD/並行実行の具体的な競合主体は未確定。調査にはcurrent Gitのsourceとローカルの再現結果を用い、CI情報や管理用Issueをローカル実装の入力にしない。

## Purpose

リポジトリ全体に存在する、一時的な実行ファイルの生成・更新・起動に伴うETXTBSY競合の危険箇所を**網羅的に検出して修正する**。既知の2テストだけの修正では完了しない。

## External feasibility

status: not-applicable

## Contract

- **リポジトリ全体**のsource（production、テスト、生成/インストール用コードと関連Shell等）から、実行可能ファイルの生成・書き込み・置換・chmod・rename・コピー・一時配置と、`os/exec`等による起動が結び付く経路を探索する。`os.WriteFile`直後の`exec`という形だけに限定せず、wrapper/launcher、生成されたスクリプト・バイナリ、並行するwriter/runner、fork/exec時のFD継承やcleanupも対象とする。
- 検出した候補を、**安全・問題あり・判断未確定**に分類する。候補ごとにsource locator、生成物のowner、書き込みFDの寿命、execとの時系列、並列実行時の危険性と処置を追跡できる形で残す。単なる検索ヒット数で網羅したことにしない。
- 既知の2例と他の危険箇所について、既存のローカル実行入口・Goテストを優先して再現・切り分ける。再現不能な競合も静的なライフサイクル検証で危険性が成立するなら修正対象とし、未再現・未確定はそのまま記録する。
- 原因に対応する実装・テスト修正を**全該当箇所**に施す。同一原因に適する共通解決があれば重複修正を避けるが、不要な抽象化や汎用frameworkは作らない。原因が異なる箇所はそれぞれ対処する。
- 見つけた危険箇所と既知の2経路を対象に、必要なfocused test、並列/反復条件、正規Go test/raceを確認する。単発PASSを非決定性解消の根拠としない。

## Must not

- 調査対象を`internal/app`と`internal/runner`、特定の2テスト、単一の生成APIに限定しない。
- ローカル再現のためにCIの履歴・Runner情報・ログや管理Issueをモデルへ渡したり、追加のCI調査から始めたりしない。
- 全体の`t.Parallel()`廃止、テスト一括直列化、無制限retry、固定sleep、実行失敗の握り潰しで隠さない。retryが必要なら対象エラーと回数を限定し、根拠を示す。
- Git authority保護、temp root分離、installed runtimeのsource identity検証を弱めない。使い捨ての独自編集スクリプトや追加Guardを作業の前提にしない。

## Acceptance criteria

- 全リポジトリの該当生成・起動経路を探索し、候補一覧と安全/修正済み/未確定の採否を証拠付きで示す。**既知の2件以外に危険箇所がないことも、探索結果で説明する。**
- 発見したすべての危険箇所に対する対処と回帰テストがあり、未再現箇所の不確実性を隠さない。既知の2件の再現条件と修正結果も確認する。
- guard/source identity等の意味を維持し、必要な正規Go test/raceが通る。ローカル再現へCI固有の追加入力を持ち込まない。

## Historical invariants

- 通常Lifecycle・source binding・独立review・必要な検証を維持する。

## Dependencies

none
