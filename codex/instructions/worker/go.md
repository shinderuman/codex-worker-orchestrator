# Go規則
- private識別子はcamelCase、public識別子はPascalCase。
- エラーには必要なコンテキストを含め、サイレント失敗より明示的なエラーハンドリングを優先する。
- Go commandは標準的な`cmd/<name>/main.go` + `internal/`構成を基本とする。
- `cmd/<name>/main.go`は起動・最終error処理だけの薄いentrypointにし、flag parsing、repository探索、validation、永続化、business logicを置かない。
- 実装責務は既存packageを優先し、同じ責務のhelper・wrapper・interfaceを並行増殖させない。
- 現行のmanaged worker sandboxでは、orchestratorのisolation設定によりBashが既定のGo build cache位置への書込みとUnix socket bind(`bind: operation not permitted`)を機械的に拒否する。この2つは実行環境側の既知capability制約であり、検証対象実装の不具合ではない。この記述は現行sandboxの保証であり、将来や別実行環境での成否を断定しない。
- build・vet確認は、対象repositoryが`$TMPDIR`配下のdeterministic GOCACHEへ固定した入口を提供する場合は必ずその入口を使い(codex-configではrepository rootの`./goquality vet`・`./goquality build`)、提供がない場合だけ`GOCACHE`を`$TMPDIR`配下へ指定して対象moduleのdirectoryで`go build ./...`・`go vet ./...`を実行する。既定cache位置のままでの再試行はしない。
- 検証義務にUnix socket bindを必要とするtestが含まれることを、repository instruction・既存testまたは一度観測した拒否signatureから既知と確認できる場合(該当例はcodex-configのglm-worker module全体test・race)、その義務はworker環境で成立しないため`parent_validation` typed pair(working dirは対象module directory)へ一度だけ委譲する。事前に既知の場合は成立確認のための失敗実行を挟まない。委譲すべき義務へskip一覧・一部packageだけの再実行、それらの成功による全体義務の代替をしない。typed pairで表現できない検証義務は未検証として報告する。socket制約が未確認の検証義務は一律に親へ委譲せず、対象packageのtargeted test・vet・buildなど通常どおりworkerで実行する。
- 既知capability制約の拒否signatureは既定cache位置への書込み拒否とsocket bind拒否だけとする。それ以外の失敗は未知の実装failureとして失敗として報告し、環境問題へ分類せず無条件retryで隠さない。
