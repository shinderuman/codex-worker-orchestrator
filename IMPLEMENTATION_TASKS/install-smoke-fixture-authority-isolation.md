# Task: install smokeのfixture authorityを対象sourceのACTIVEから分離する

## Original instruction

親Codexの中間評価による採用要求:

````text
installer smokeの通常Task fixtureを、インストール対象sourceの現在のACTIVE Taskの実行指定から分離する。parent-onlyの評価TaskがACTIVEであることを、通常workerの起動失敗というinstaller回帰へ読み替えない。sourceのTaskやproduction admissionを変えて通す処理は追加しない。正常利用を表す正規fixtureを明示し、そのauthorityとsourceが混同されないことを検証する。
````

共通のユーザー禁止事項:

````text
フローを通すために実装を変えることは許可しない
````

## Amendments

none

## Resolved references

- `review-evidence/codex-efficiency-webgpt-adoption-2026-11-03.md`のfixture authorityに関する採否。
- 対象ownerは`tests/install_smoke.sh`の検証用repository構築と通常Task dispatch。productionのTask実行指定は変更対象にしない。

## Purpose

installerの検証対象と、開発中repositoryの実行指定を混同する再失敗を防ぐ。

## External feasibility

status: not-applicable

## Contract

- インストールするproduction sourceは実際の検証対象のままにし、通常Taskを実行するfixtureに必要なcanonical authorityを明示する。
- Task/Planの自動修復、parent-onlyからglmへの降格、source Taskへの上書きを使わない。
- fixture専用のstate machineや第二installerを作らず、既存の本体・正規入口・lock/processの実検証を保持する。
- 実装前に対象sourceとfixture authorityの所有境界を親Codexが確認する。

## Must not

- 今回のbatchの完了条件を成功へ書き換えるために実装しない。
- production guard、実行指定、confinement、Git hook、検証回収を緩めない。
- 旧形式・旧実行経路との互換fallbackを追加しない。

## Acceptance criteria

- source側のACTIVEが通常Task・parent-onlyのいずれでも、installer対象sourceを変えずに適切な通常Task fixtureを検証できる。
- malformed source、インストール失敗、実dispatch失敗を成功へ変換しない。
- 原repositoryのTask/Plan/実行指定が不変であり、既存smokeのlock・process・export検証が維持される。
- 関連testと全体Lintを通し、fixture成立の証拠とproduction成立の証拠を区別する。

## Historical invariants

- current schema/current ownerだけを正とし、後方互換性を持たせない。
- source変更を伴わない品質検証の独立性を保つ。

## Dependencies

none
