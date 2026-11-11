# Task: macOSの標準的な/var経由パスをuninstallが誤拒否する問題を解消する

## Original instruction

2026-11-11 ユーザー指示（原文）:

```text
次の2件Auditしろ
今回はもしPR起票したいやつがあっても俺に報告をしろ
```

2026-11-11 ユーザー指示（原文）:

```text
Issueの起票ってAudit Issueも作ってないとか言わないよな？
```

2026-11-11 Task化指示（原文）:

```text
タスク化のPRつくれ
```

## Amendments

none

## Resolved references

- 監査対象は原本 `a7c4d036-62d0-44f2-80e8-625959f2add6.zip` と `e9c9beb6-5fd6-4d36-8eee-8759a4df96ca.zip`。前者の独立した新規Findingを[Dogfood Audit #1548](https://github.com/shinderuman/codex-worker-orchestrator/issues/1548)に記録。後者の[Dogfood Audit #1549](https://github.com/shinderuman/codex-worker-orchestrator/issues/1549)には独立した新規Findingなし。
- 前者の`install-smoke`の初回Run `ea3d0fddf1a97ce7cc75396a6c1fa187` は `uninstall incomplete: refusing symlink path component: /var` / `exit status 1` で失敗し、`/private/tmp`配下を使った後続Run `d4cf0e41904c5e3e466a5ab011ade711` はPASS。macOSでは `/var` が `/private/var` を指す標準symlink。
- 対象ownerは `glm-worker/internal/uninstallcmd/run.go` の `rejectSymlinkComponents` と、その呼出元のuninstall path admission。正規のinstall/uninstall/reinstall smokeで発見。以前の `installation-managed-uninstall.md` Taskは完了・退役済みであり、再Openしない。
- 前者の他の異常（Publication親actionの失敗、post-install probeのcollector欠測）はそれぞれ現存する `post-publication-parent-terminal-regression-observed.md`、`post-install-probe-live-bundle-fidelity-regression.md` の責務であり、本Taskに取り込まない。

## Purpose

macOS標準の一時ディレクトリ経路に含まれるOS管理の `/var` symlinkを理由に、正規のuninstall / install-smokeが失敗する問題を修正する。インストール所有権・改ざん検査と、ユーザー管理/不明なsymlink経由の危険な撤去の拒否は維持する。

## Contract

- 失敗Run原本と現行 `uninstall.sh` / `uninstallcmd` / install-smoke / managed owner検査を照合し、正規のOSパス別名と危険なsymlinkを区別する責務境界を確定する。
- `/var`→`/private/var` を含む標準的なmacOSのpathでも、正規に所有しているinstall artifactを退役できるようにする。path canonicalizationの前後で検証対象・削除対象が逸れないことを保証する。
- 任意のuser-controlled symlink、変更済み/非所有file、パス置換や想定外のrootへの脱出は引き続き拒否する。既存のshared install/uninstall lockと所有物のretirement contractを壊さない。
- 正規 `install→uninstall→再uninstall→reinstall`、標準 `/var` 経由、`/private/var` 経由、危険なsymlinkの正負テストを追加し、実macOS上の失敗経路を検証する。通常Go/race、必要なinstall-smoke、required CIも通す。

## Must not

- symlink拒否自体を全面撤去したり、入力pathのあらゆるsymlinkを無条件許可しない。
- symlinkを無条件に解決しただけで安全性が維持されたとみなさない。実装に不要な互換経路、新しいGuard、第二の所有権authorityを追加しない。
- 既存完了Taskをreopenせず、別責務のPublication・probe collectorを変更しない。

## Acceptance criteria

- 原本で失敗したmacOS `/var/folders/.../T` 配下を用いる実uninstall / install-smokeがPASSし、`/private/tmp` の正常経路と同等に扱われる。
- malicious / unexpected symlink・非所有物・改ざん・パス境界逸脱はfail-closedのまま。install/uninstall/reinstallの既存正負シナリオが退行しない。
- 原因と変更ownerが単一に絞られ、Darwin実環境とGo/race・required CIで再現前FAIL→修正後PASSを確認する。実環境未検証ならそれを明記し、完成と誤認しない。

## Darwin acceptance

- `./internal/uninstallcmd:TestUninstallAcceptsCanonicalMacOSVarAlias`

## Historical invariants

- `IMPLEMENTATION_RULES.md`、管理対象の所有権確認、shared lock、Task 1 Commit、独立review、旧形式互換禁止、Task完了時の退役を維持する。

## Dependencies

none
