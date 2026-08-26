# install smokeのscenario分類とsemantic coverage

`tests/install_smoke.sh`は、production `install.sh`のtest実行契約を変更せずに、
real `go test ./...`を実行する代表scenarioと、go shimで呼出contractだけを検証するscenarioへ分離する。
本fileの秒数・回数は測定時点の観測値であり、恒久的な品質thresholdではない。

## 実行境界

- real preflight: 対象scenarioの`install.sh`をgo shimなしで実行し、preflightの`go test ./...`(glm-worker・tools/merge-json両module)・commentlint・build全部を本物のtoolchainで実行する。
- contract preflight: `make_go_shim`が`go test`呼出を記録・模擬し、`go build`・`go run`は本物へ委譲する。install.shが正しいmoduleへ正しい順序でtestを起動し、失敗exitを管理file配置前にfail closedで伝播する契約を、`expect_preflight_failure`の期待log比較と`expect_go_test_contract`の起動回数検証で固定する。
- 本物のGo suite自体の合格は、repoの通常test gate(`go test ./...`・`-race`・`go vet`)とreal preflight代表実行が保証する。go test失敗のinstall拒否伝播は、shim強制失敗scenarioとuntracked plan実測失敗scenarioの両方で固定する。

## scenario分類

installer呼出43回(成功2・upgrade2・preflight失敗6・settings系11・plan gate失敗17・plan gate成功5)。

| scenario | semantic保証 | preflight |
|---|---|---|
| success 1回目(清掃install) | 本物toolchainでcommentlint・全test・buildが走りfail closedで管理file配置、binary動作・全管理file配置・merge | real |
| untracked plan(install拒否) | plan gate skip後も本物suiteの自己保護test(`TestImplementationPlanFileIsTrackedCanonical`)がuntracked planを検出しinstallが拒否、`bin`未配置 | real(失敗) |
| success 2回目(idempotent再install) | manifest掃除・local file保存・binary unchanged | contract |
| upgrade×2 | managed値置換・local保存・idempotent | contract |
| preflight失敗5種(commentlint・glm-worker test・glm-worker build・merge-json test・merge-json build) | 各段階の失敗がgo呼出順序付きでfail closed、管理file・hook無変更 | contract(強制失敗) |
| claude probe失敗 | 全preflight通過後の`--json-schema`検証失敗がfail closed、probeは`--help`だけ | contract |
| override系(override・削除・malformed・null×2・復元・broken state) | settings merge・env state sidecar・fail closed・idempotent | contract |
| plan gate失敗17種 | plan検証がpreflight前に拒否、go呼出なし・log断言 | なし |
| plan gate成功(amend復旧・synced×2・positive) | 検証済みfinal HEADからinstall継続・binary配置 | contract |

## 測定(2026-08-26, darwin/arm64)

測定方法: repo外に置いた`go` wrapperが実際のtoolchain呼出をmodule・subcommand付きで記録し、full smokeのwall-clockとreal `go test ./...`到達回数を数えた。logは測定日のsession artifactへ保存した。

- 改善前: wall-clock 1418s(23.6分)、real `go test ./...` 44回(glm-worker 25・merge-json 19)、installer呼出43回、scenario数不変、`install smoke: PASS`
- 改善後: wall-clock 302s(5.0分)、real `go test ./...` 3回(glm-worker 2・merge-json 1)、installer呼出43回、scenario数不変、`install smoke: PASS`

## coverage非退行の根拠

- production `install.sh`は無変更であり、本番installがpreflightで`go test ./...`を実行する契約・失敗時fail closedは変更されない。
- 削除したscenarioはなく、意味的assertion(配置grep・idempotency・fail closed・go呼出順序・plan gate拒否理由)は全て残る。従来fail検出に使っていた注入test file 2件は、shimのmodule単位強制失敗とuntracked plan実測失敗へ置き換え、期待logは同一形式のまま比較する。
- real実行2件(清掃install成功・untracked plan拒否)が「本物suiteの実行と失敗伝播」を代表し、他scenarioはinstall.shの呼出contractをshim logで固定する。同一sourceに対するGo suiteの反復実行は品質証拠を増やさないため、semantic coverageを落とさずに反復を削減できる。
