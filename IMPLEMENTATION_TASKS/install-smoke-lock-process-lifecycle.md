# Task: Install smokeのlock保有process終了

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
lock helperの実processを正常時・失敗時とも終了させる。
go run wrapperではなくbuild済みhelperを起動し、PID・signal・waitをlock保有processへ束縛する。
既存のsmoke cleanup/trapへ統合し、失敗途中にも孤児processや保持lockを残さない。専用daemon/process管理基盤を追加しない。
````

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1485#issuecomment-6588570580

````text
Codexによる採否判断です。確認対象はhead `3bf6dfa20cba2f157f03994b74431bdf175f8ed4`、変更scriptのblobは `95b90435277fbc8283241470a4940845f32791aa` です。

**build済みhelperを直接background起動し、そのPIDへkill/waitを束縛する方式は採用します。現headは下記1点を修正してから最終採用としてください。**

### 1. processの所有・終了

shellが保持する `$!` とlock保有Go processが一致する構成になっており、go run wrapperのPIDだけを終了する元の問題に対する修正として妥当です。通常経路と強制 `exit 19` のsubshellで同じcleanupを使い、helper終了後のPID不在とlock再取得を確認する構成も採用します。HUP/INT/TERMをexitへ接続し、EXIT cleanupでwriter・holderを回収する方針を維持してください。専用daemon/process管理基盤への拡張は不要です。

### 2. 要修正：lock競合とprobe故障を区別する

現在の `--try` helperはopen失敗、flock失敗等をすべてexit 1にし、`wait_for_export_lock` はprobeの任意のnonzeroを「lockが競合している」として成功扱いします。これではprobeそのものの故障を競合の証拠へ読み替え得ます。

非blocking flockの **EWOULDBLOCK/EAGAINだけを競合専用の終了状態**に分け、呼出側はその終了状態だけを競合として受理してください。成功は0、それ以外のopen/I/O/起動失敗等はsmoke失敗として診断を残します。cleanup後のprobeは0であることを引き続き要求してください。成功/競合/故障を実結果で区別し、単に `if probe; then fail; else success` のままにしないこと。

既存のlocked出力、boundedなreadiness待機、実concurrent exportとarchive assertionは維持します。sleep経過を成功条件にはせず、fixtureだけを検証する別の大きなテスト基盤も追加しません。

### 3. CIと本配置確認の扱い

[Install Smoke #41524338629](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41524338629)の4ジョブsuccessと、installer jobの `install smoke: pass` を確認しました。検証commit `c67f7470...` と現headのscript blobが一致し、両commit間の差がworkflowの一時push triggerの撤去だけであることも確認済みです。したがって、これは**現scriptに対するLinuxの実行証拠として採用**します。

一方、[run #41523203535](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41523203535)はquality内のrepository lint/vet/buildがsuccessですが、appはcancelled、集約lintはfailure、install-smokeはskippedです。「このrun全体がPASS」とは扱いません。

本Taskはsmoke scriptだけの変更で、production binaryやmanaged instructionを変更しません。**このTaskのためだけの実ユーザー環境への再installを、新たな必須条件には追加しません。** 既存の隔離installer smokeが本件の正規検証です。今回CodexがDarwinでこのPRのscriptを実行したわけではないため、Darwin実測済みとは表現しないでください。

上記probe修正後はscript blobが変わるので、変更後scriptに束縛した正規installer smokeと必要なlint/必須CIを成立させ、結果を提示してください。関係のないGo挙動を変更したり、CIを緩めて通したりする必要はありません。

### 4. 完了Metadata

現時点では要修正・未完成です。修正後の差分と検証結果をCodexが確認して全Acceptanceを採用してから、親CodexがTask削除とPlanの当該参照だけを同期します。Web GPT側で先行削除しないでください。checkpoint先頭の順序は維持し、この返答ではMerge/Closeは行いません。
````

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
