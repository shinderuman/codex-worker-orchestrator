# Task: Install smokeのlock保有process終了

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
lock helperの実processを正常時・失敗時とも終了させる。
go run wrapperではなくbuild済みhelperを起動し、PID・signal・waitをlock保有processへ束縛する。
既存のsmoke cleanup/trapへ統合し、失敗途中にも孤児processや保持lockを残さない。専用daemon/process管理基盤を追加しない。
````

## Amendments

none

## Resolved references

- PR #1343 / comment 4242081421: go run/subshellのPIDだけを停止し、SIGTERM待ちの実lock保有childを終了保証していない。起動・失敗cleanup・waitを同じ実processへ束縛する。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `tests/install_smoke.sh:EOF_HOLD_LOCK`

## Purpose

lock helperの実processを正常時・失敗時とも終了させる。

## External feasibility

status: not-applicable

## Contract

- go run wrapperではなくbuild済みhelperを起動し、PID・signal・waitをlock保有processへ束縛する。
- 既存のsmoke cleanup/trapへ統合し、失敗途中にも孤児processや保持lockを残さない。専用daemon/process管理基盤を追加しない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- 正常終了と途中失敗の双方でhelper不在とlock再取得を確認する。
- 実concurrent export smokeの検証目的を弱めず、固定sleepで成功を仮定しない。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
