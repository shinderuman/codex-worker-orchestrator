# Task: 一時実行ファイルのETXTBSYによる非決定的失敗を解消する

## Original instruction

2026-10-29 ユーザー指示（原文）:

```text
この最後の方にテストがコケてるやつってさっき起票してるやつと一緒じゃねえか？
一緒だとしたらさっきのIssueを修正してPRを作れ
修正はIMPLEMENTATION_PLANの起票だ
次のActiveで作業する
なおローカルで再現する場合CIの情報とかを見せる必要はない
見せると余計な作業が発生するからローカルで再現させて解消したほうが早い
```

## Amendments

none

## Resolved references

- 同じエラー種別は `fork/exec ...: text file busy` (Linux `ETXTBSY`)。具体的な競合FD・単一root causeは未確定で、2件を同一の実装修正責務として扱う。
- `glm-worker/internal/app/controller_completion_install_test.go` の `TestInstalledCompletionRuntimeRequiresExactCleanSource/same`: `os.WriteFile`で作成した実行可能ファイルを `installedCompletionRuntime` 経由で直後に起動した際に失敗。
- `glm-worker/internal/runner/git_authority_temp_root_test.go` の `TestGitAuthorityClaudeWrapperOwnsProcessTempRoot`: `prepareClaudeWrapper` が生成した実行可能な `claude-with-git-authority-guard` の起動時に同じエラー。生成ownerは `glm-worker/internal/runner/git_authority_claude_wrapper.go`。こちらはproductionのWrapper生成経路を使う。
- これらは調査入口であり、実装時はcurrent Gitのコードと実行経路を正とする。ローカル再現へ管理用IssueやCIログを持ち込まない。

## Purpose

一時実行ファイルの作成と起動の境界で起きる非決定的なETXTBSYを、ローカルで原因切り分けして修正し、両経路の回帰を防ぐ。

## External feasibility

status: not-applicable

## Contract

- 既存のローカル実行入口・Goテストを使って両失敗箇所を再現・切り分ける。必要なら実行OSや並列度を調整して条件を揃える。再現できない場合も試した条件と分かったことを記録する。
- 一時実行ファイルの書き込み・close・`exec`、並行プロセスとFD継承、生成物のlifecycleを確認し、確定した原因に即して修正する。2件が異なる原因なら、それぞれ適切に修正する。
- 修正後は両経路のfocused testと関連する正規test/raceで検証する。一度のPASSだけを非決定性の解消証明にしない。必要な反復・負荷条件は過度に広げず対象に絞る。

## Must not

- ローカル再現のためにCIの履歴・Runner情報・ログをモデルへ渡したり、追加のCI調査から始めたりしない。
- 全体の`t.Parallel()`廃止、テスト一括直列化、無制限retry、固定sleep、実行失敗の握り潰しで隠さない。retryが必要なら対象エラーと回数を限定し、根拠を示す。
- Git authority保護、temp root分離、installed runtimeのsource identity検証を弱めない。独自の使い捨てスクリプトや新しいGuardをこの修正の前提にしない。

## Acceptance criteria

- 両経路のETXTBSYの原因・再現条件（未再現条件を含む）が説明され、局所的な修正と回帰テストがある。
- 実行ファイル検証・guardの意味を保ち、既存の正規test/raceが通る。
- CIの情報をローカル再現への余計な入力として使用していない。

## Historical invariants

- 通常Lifecycle・source binding・独立review・必要な検証を維持する。

## Dependencies

none
