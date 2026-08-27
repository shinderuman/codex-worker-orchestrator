# capability必要quality gateの実行境界

親Codexが自身の検証・final gateでquality gate commandを直接実行する場合だけ適用する。GLM worker/reviewerの実行契約は`~/.codex/instructions/glm-execution.md`に従う。

## Unix socketを必要とするGo full suite

このrepositoryの`glm-worker` module full suiteにはUnix socket bindを必要とするapp系testがある。Codex sandbox内では`listen unix ...: bind: operation not permitted`で成立しないため、module rootから次の固定入口だけを使う。

```sh
glm-worker --quality-gate go-test
glm-worker --quality-gate go-test-race
```

- 入口はそれぞれ`go test ./...`、`go test -race ./...`の固定argvだけを実行する。追加argvはusage errorでfail closedする。
- 子processの`GOFLAGS`は空に固定する。
- 実行dirは呼出時current directoryで、入口側でrepositoryを選び直さない。
- 成功はstdoutのJSON object 1件、失敗はstderrの`kind:"quality_gate_failed"` error JSONとnon-zero exitで返す。subprocess出力はlog fileへ保存し、machine出力にはpathだけを載せる。
- sandbox内で一度失敗させてから同じsuiteを再実行せず、最初からこの入口へ一度だけdispatchする。

## sandbox内で実行するgate

`go vet ./...`、`go build ./...`、gofmt、`harnesslint`、`commentlint`、Shell lint、`git diff`等、追加capabilityを必要としないcommandはsandbox内で実行する。capability根拠のないcommandへ昇格権限を広げない。

`harnesslint`は`codex-worker-orchestrator`固有のrepository quality gateで、Go/Shell/Markdown/structured configとgate wiringを検査する。通常のGLM workflowではreviewerを呼ぶ前にwrapperがcheck-onlyで実行し、不合格ならreviewerへ進めない。formatter等の自動修正が必要ならworker側で`harnesslint --fix`を実行し、その後checkを通す。

installer/managed-file behaviorを変更した場合のoffline install smokeは`glm-worker --install-smoke --role <role>`で実行する。証拠cacheや再利用layerは持たず、必要なときに実行結果そのものを証拠とする。

## fail closed

- 昇格境界で実行したfull suiteの失敗は実装不具合として扱い、sandbox由来と推測してskip・成功扱いしない。
- 新しいcapability不足を一次証拠で確認した場合だけ固定入口へ最小追加する。汎用command実行formや直接`go test`をallowするprefix ruleは追加しない。
- 同一snapshotの同一gateを環境選択だけを理由に重複実行しない。
