# Go規則
- private識別子はcamelCase、public識別子はPascalCase。
- エラーには必要なコンテキストを含め、サイレント失敗より明示的なエラーハンドリングを優先する。
- Go commandは標準的な`cmd/<name>/main.go` + `internal/`構成を基本とする。
- `cmd/<name>/main.go`は起動・最終error処理だけの薄いentrypointにし、flag parsing、repository探索、validation、永続化、business logicを置かない。
- 実装責務は既存packageを優先し、同じ責務のhelper・wrapper・interfaceを並行増殖させない。
- ビルド確認は対象moduleで`go build ./...`を基本とする。
