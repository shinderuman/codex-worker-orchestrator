# Task: goquality未追跡ディレクトリのsource検出とDocker実行の整合

## Original instruction

2026-11-01 Dogfood Audit（Bundle `716e1f06-9150-4b76-b83d-5638ad5c538c.zip`）で検出した、CR-1401-7修正後のsource snapshot / Docker backend不整合を正規Taskへ登録する。

## Amendments

none

## Resolved references

- 完了済みTask `external-review-findings-1401-1441.md` がCR-1401-7の `goquality` shell snapshotを改修し、`goquality:source_snapshot` で未追跡のdirectoryに `directory` 固定markerを記録するようにした。公開commit `3227c5e747737736049062ccd5c78c92db08d9c3`。
- `glm-worker/internal/goqualitycmd/script_test.go:TestGoqualityScriptSnapshotHandlesUntrackedNestedRepositoryAndDanglingSymlink` は、未追跡のnested Git repositoryとdangling symlinkを含むfixtureでPASSする。ただし当該testはDocker Linux/arm64の全体Go test内で動き、`goquality`のDarwin arm64→Docker分岐を通らない。
- `glm-worker/internal/dockertest/source.go:sourceArchive` は `git ls-files --cached --others --exclude-standard -z` の返却を渡す。Gitはnested untracked repositoryを `nested/` のようなdirectory entryとして返す（使い捨てGit fixtureで直接確認）。`writeArchive -> archiveEntry -> archiveHeader` は `allowDeleted && info.IsDir()` で `submoduleのsource転送は未対応です` を返す。したがってMacの正規Docker backendではsource snapshotのshell修正だけでは当初の入力ケースが通らない。
- shell側はdirectory名と固定markerしかsource digestに入れないため、nested directory内のuntracked sourceを変更してもsource digestが同じとなり、Linux/native診断で `source_changed:false` と誤認し得る。どこまでが正規source evidence対象であるかは実行backendとsource policyに基づき決定する。
- CR-1401-7原指摘は `goquality test` がnested untracked Git repository / dangling symlinkでテスト開始前に異常終了する問題。修正済みとされた証拠は対象Go testのPASSであり、Darwin→Docker実呼び出しのPASSではない。後続Live Mac実行での同事象再発は未観測。
- 既存 `worker-docker-test-owner-isolation.md` はreal socketをsandboxから隔離する権限ownerであり、本Taskのsource内容とbackend同等性を代替実装しない。重複するDocker owner変更を避ける。

## Purpose

Goテスト実行前後のsource snapshotとDocker source transferが、同一入力集合・無視対象・拒否条件について一致するようにする。未追跡ディレクトリがあるだけで不必要に正規診断を停止させない一方、実際にテストへ供給されるsourceの変化を `source_changed:false` と偽ってはならない。

## External feasibility

status: not-applicable

## Contract

- 現行 `goquality` shell `source_snapshot`、`dockertest.sourceArchive`、`writeArchive`、Gitの `--cached --others --exclude-standard -z`、native/Docker backend切替、既存 `ignored_inputs_absent` evidenceの意味を横断して、どの入力がテストsourceに含まれるか明確にする。
- 一つのcanonical source scopeに沿って、nested untracked repository、通常のuntracked directory、dangling symlink、相対symlink、既存split indexの各ケースで正常に処理するか、sourceの意味が保証できない場合は理由のあるfail-closedにする。正規診断が適用不能な対象を単に `directory` markerで通過させない。
- 特にnested untracked directory内部の変更を前後snapshotが検知できるか、明示的に診断sourceの対象外ならDocker archive側もそれを供給しないことと無視扱いをmachine evidenceへ示せるか、検証する。source同一性に影響する対象を黙って無視しない。
- Darwin arm64の正規Docker pathとLinux/native pathでsource挙動を整合させる。既存の `./goquality test` の入口、必要なDocker/Go1.25.4 contract、正規validationの実行方式を変更・回避しない。
- 単に`submoduleのsource転送は未対応です`を別の無意味なerrorへ置換したり、archive検査を削除して見かけのPASSへ変換しない。既存ownerで必要十分な修正にとどめる。

## Must not

- WorkerへのDocker socketの再開放、任意Docker API/host bind mountを導入しない。
- 事実と異なる `source_changed:false`、`ignored_inputs_absent:true`、架空のsource identityで正規検証を通さない。
- ディレクトリ全走査や巨大な別snapshot frameworkを無条件に追加しない。必要なsource範囲とコストを先に確定する。
- CR-1401-7以外の指定されていないPRレビュー範囲・quoted filename等の独立問題を混入させない。
- 過去の成功テストや現在のDocker security ACTIVEを理由に無条件にNo Changeとしない。current sourceで有効性を評価し、既に同一正規ownerが具体的経路を解消している場合は重複修正しない。

## Acceptance criteria

- 正規のDarwin arm64→Docker diagnostic入口で、untracked nested Git repositoryとdangling symlinkのfixtureを使い、実際のsourceArchive/Go testの終了結果を確認する。Dockerが利用できない環境では必要な環境制約と未検証を記録し、Linux/nativeのPASSをDockerの代用としない。
- Linux/nativeで、テスト実行中にuntracked nested repository内のファイル内容を変更する負caseを再現し、source policyに従って変更が検知されるか、正規source対象外である理由が証拠化されることを確認する。
- 正常な未追跡ファイル、directory/symlink、変更なしのワークツリー、テスト中のsource変更、split indexの正負境界がsource-boundのまま維持される。
- 正規Go/test・race/品質gate、source archiveの検証・独立Reviewを実施する。ユーザーの実運用上のpost-fix成功が未観測ならその限界を明記する。

## Historical invariants

- Source/Task/attempt authority、独立Review、最終品質gate、親Codexのmeaningful semantic decision、1 Task 1 Commitを維持する。
- 最上位目標はCodex ReductionとQuality Delta。GLMのトークンだけの削減目的で新たな機械層を導入しない。

## Dependencies

- `IMPLEMENTATION_TASKS/worker-docker-test-owner-isolation.md`
