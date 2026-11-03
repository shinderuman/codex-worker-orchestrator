# Task: Install Smokeのfixture正規化で元sourceの不正を隠さない

## Original instruction

`IMPLEMENTATION_TASKS/install-smoke-fixture-authority-isolation.md`（実行前Git `018e03e8f57b835ed412f73e1f84ba0d02ed3596`）の原要求から、未充足の箇所を引き継ぐ。

> インストールするproduction sourceは実際の検証対象のままにし、通常Taskを実行するfixtureに必要なcanonical authorityを明示する。

> source側のACTIVEが通常Task・parent-onlyのいずれでも、installer対象sourceを変えずに適切な通常Task fixtureを検証できる。

> malformed source、インストール失敗、実dispatch失敗を成功へ変換しない。

## Amendments

none

## Resolved references

- 2026-11-04 Dogfood Audit [#1498](https://github.com/shinderuman/codex-worker-orchestrator/issues/1498) F01、Bundle `903478d9-2131-46ef-9c1f-196c03f38436.zip`。
- 実装済み `39cf5ac9e5a6c2694dc876b1cb1ee65cede0a8dd`、`tests/install_smoke.sh:39-68,110-121`: `rsync`でsourceをコピーした後、`$repo/IMPLEMENTATION_TASKS`を全削除し`$repo/IMPLEMENTATION_PLAN.local.md`をfixture内容で上書きし、その`$repo/install.sh`を実行する。
- `install.sh`は`plancheck`を`$repo_root`に対して実行する。そのためsource側のPlan/Taskがmalformedでも、install前に置き換えられると失敗を検出できない。Workerのpositive fixturesとReviewer PASSはこのnegative boundaryを直接検証していない。
- 元Taskのfixture分離は既存の正当な目的であり、source ACTIVEの通常Task／parent-only双方に対する成功経路は保持する。

## Purpose

検証用の通常Task authorityをsourceのACTIVEから分離しつつ、インストール対象の元sourceに存在する不整合を正常なfixture定義で覆い隠さない。

## External feasibility

status: not-applicable

## Contract

- `tests/install_smoke.sh`および`install.sh`／`plancheck`の正規source・fixture・controller activation境界を確認し、元sourceを検証する時点とfixture authorityを適用する時点を明確に分離する。
- 元sourceのPlan/Task不整合が正規install検査で拒否されることを、fixture用Plan/Taskの作成前または独立した同等の正規検査で保証する。単にfixtureに書き換えた後の`plancheck`成功を元sourceの検証成功と呼ばない。
- source ACTIVEの通常Task／parent-onlyどちらでも、既存のfixtureの正規worker dispatch、Task identity/digestの結合、installer、lock/process/exportの実検証を維持する。
- source authorityとfixture authorityを混同する別のwrite/restore pathを増やさず、既存CLI・検証owner・shell fixtureを再利用する。

## Must not

- production Task/Planを勝手にrepair・書換えして成功とする、またはmalformed sourceを正しいfixtureで覆い隠してPASSへ変換する。
- source側parent-onlyを通常GLM Taskへ降格しない。production admission、品質Guard、confinement、Git hook、失敗検証を弱めない。
- 第二installer、第二state DB/daemon、旧schema互換fallback、テスト選択やscenario削除を導入しない。

## Acceptance criteria

- 元sourceのPlanがmalformed、Task参照が不成立、通常Taskが不整合という負例で、fixture正規化前にsourceの正規検証がFAILとなることを実CLIで示す。既存production codeの構文不良・installer失敗・dispatch失敗もPASSへ変換しない。
- 正常な元sourceのACTIVEが通常Task・parent-onlyの各場合に、fixtureが通常Task authorityへ独立して束縛され、必要なinstall smoke全体がPASSする。
- fixtureの専用Task・Planだけがfixture側の操作として使われ、元sourceのTask/Plan/実行指定と、検証対象production sourceの不正を隠さず、既存lock/process/exportとTask digestの検証を保つ。
- 変更前後の正負の再現証拠、全体Lint、関連テスト、本配置smoke、独立Reviewをsourceへ束縛する。

## Historical invariants

- 現行sourceと同じ必要な品質保証を維持し、false successをなくす。原Taskの達成済み「fixture authorityの独立」を取り消さない。

## Dependencies

none
