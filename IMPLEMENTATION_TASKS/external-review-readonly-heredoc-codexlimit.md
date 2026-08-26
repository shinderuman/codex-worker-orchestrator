# Task: external reviewのF1–F3を修正する

## Original instruction

```text
REVIEW
全件を現HEADで確認し、未解消なら対応すること。
重要度による選別はしない。解消済みならスキップ。
既知のcommentlint --fix空行問題は本review対象外。
親管理metadataは変更しない。commit/pushしない。

F1 glm-worker/internal/runner/runner.go:ClaudeRunner.Run / internal/workflow/externalfeasibility.go
問題: PoC/observationのReadOnlyはEdit/Write/NotebookEdit/Agentしか禁止せずBashがwrite可能。終了snapshotは事後検出でありwrite防止ではなく、write→restoreも検出不能。
要求: PoCを実際にwrite不能な境界へ隔離する。実Bash write拒否のproduction-path testを追加。

F2 glm-worker/internal/commentlint/commentlint.go:heredocPattern,scanShell
問題: quoted文字列中の<<EOF等をheredoc開始と誤認し、terminatorが無ければ以降の全行をscanしないためcomment invariantをbypassできる。
要求: shell contextを考慮したheredoc認識へ修正し、quoted fake heredoc/unterminated pendingのnegative testを追加。

F3 glm-worker/internal/codexlimit/codexlimit.go:exchange
問題: initialize/initialized/rateLimits-readをresponse待ちなしでpipelineしておりapp-server handshake contractと不一致。既存fake testはstdinを読まず検出不能。
要求: initialize response確認後にinitialized、その後rateLimits/readを送る逐次handshakeへ修正し、request順序をassertするfake-server testを追加。

全件処理後に必要なverificationを行い、ACTIVE taskを継続すること。
```

## Amendments

none

## Resolved references

- 「現HEAD」はreview受領時の`510b9bb`を指す。
- 「ACTIVE task」は`IMPLEMENTATION_TASKS/markdown-context-footprint-reduction.md`を指す。
- `commit/pushしない`はGLM workerへ適用し、親Codexは既存恒久ruleどおりgate完了後にlocal commitとmainへの通常fast-forward pushを行う、という後続ユーザー訂正を適用する。

## External feasibility

status: not-applicable

## Purpose

現HEADで成立したF1–F3を既存contractへ最小修正し、隔離follow-up完了後に停止中ACTIVE taskを同一sessionから再開する。

## Contract

- F1: PoC/observationのBashを含むproduction tool実行を実際にwrite不能な境界へ置き、write→restoreで回避できないことをproduction-path testで固定する。
- F2: shell quote/context外の実heredoc operatorだけを開始として認識し、quoted fake heredocやunterminated pendingが後続comment scanをbypassしないことを固定する。
- F3: initialize request、対応成功response、initialized notification、rateLimits/read requestの順序をproductionとfake server testで一致させる。
- 全3件を現HEADで検証し、解消済みのものは変更しない。

## Must not

- 既知のcommentlint `--fix`空行問題を混ぜない。
- unrelated refactor、generic sandbox/parser/RPC frameworkへ拡張しない。
- GLMは親管理metadata・commit・pushを行わない。

## Acceptance criteria

- F1–F3の要求されたnegative/production-path testが通る。
- 関連test、全test、race、vet、build、gofmt、既存comment lint gateを通す。
- 独立reviewと親Codex最終semantic reviewを通す。
- 親Codexが隔離成果をmainへ統合・commitし通常fast-forward pushした後、停止中ACTIVE taskをresumeする。

## Historical invariants

- 最上位目的と既存safe-stop/isolation保持contractを維持する。

## Dependencies

none

## Review findings

none

## Current boundary

現HEADの一次確認でF1–F3はいずれも未解消。停止中ACTIVE taskを保持した隔離worktreeで処理する。
