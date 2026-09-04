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
- gate開始前に`validation_run_id`とexact snapshot（repository identity / HEAD / index digest / worktree digest）をstateへ保存する。
- gateを新規開始または既存runへattachした直後、stderr JSONLの`quality_gate_started` control eventで`validation_run_id`と`attached`を通知する。stdoutのterminal resultは従来どおり単一JSON objectのまま保持する。
- 成功はstdoutのJSON object 1件、失敗はstderrの`kind:"quality_gate_failed"` error JSONとnon-zero exitで返す。subprocess出力はrun ID単位のlog fileへ保存し、machine出力にはpathだけを載せる。
- 同じformかつ同じexact snapshotのrunが`running`なら新しいgateを起動せず、そのrunへattachする。completed pass/failはcacheとして再利用せず、明示された新規実行は新しいrunとして扱う。
- task finalizationの`glm-parent-action finalize-check <form>`は、current `--handoff`の`routing_evidence`（同一form・PASS・同一repository・同一HEAD/index・同一implementation worktree digest。parent-managed metadataだけの差分は`parent_metadata_only`として許容し、exact一致は`exact`）の`working_dir`を、repository配下かつ`go.mod`を持つmodule rootであることを機械確認した上でfresh gateの実行dirへ使う。evidenceが不存在・解決不能・module root不成立などの通常の利用不能ならcaller cwdを同じmodule root検証へ通し、evidenceもcallerも成立しなければ`stage:"routing"`・`reason:"no_module_root_working_directory"`のstructured failureとしてgateを起動せずfail closedする。ただしevidenceのworking_dirがrepository外を指す場合は利用不能ではなくstate/authority不整合として扱い、callerへfallbackせず`stage:"routing"`・`reason:"routing_evidence_outside_repository"`でfail closedする。選択dirと根拠は結果JSONの`routing`へ載せ、completed validation自体をcacheとして再利用しない。
- 呼出元のterminal/tool sessionを失った場合は、開始時に通知されたrun IDを使って`glm-worker --quality-gate status <validation_run_id>`、`watch <validation_run_id>`、`result <validation_run_id>`から同じrunの状態・完了結果・evidence pathを回収する。親が周期pollする運用にはしない。
- sandbox内で一度失敗させてから同じsuiteを再実行せず、最初からこの入口へ一度だけdispatchする。

## sandbox内で実行するgate

`go vet ./...`・`go build ./...`相当の親final gateはrepository rootの固定入口`./goquality vet`・`./goquality build`をsandbox内で実行する。`goquality`は`quality-tools.yml`からrepository-authoritative Go versionを読み、`harnesslint`と同じ`${TMPDIR:-/tmp}`配下のdeterministic `GOCACHE`を使う。親Codexがtaskごとのcache pathを合成せず、homeのGo build cacheへ触るためだけにescalateしない。

gofmt、`harnesslint`、`commentlint`、Shell lint、`git diff`等、その他の追加capabilityを必要としないcommandもsandbox内で実行する。capability根拠のないcommandへ昇格権限を広げない。

`harnesslint`は`codex-worker-orchestrator`固有のrepository quality gateで、Go/Shell/Markdown/structured configとgate wiringを検査する。通常のGLM workflowではreviewerを呼ぶ前にwrapperがcheck-onlyで実行し、不合格ならreviewerへ進めない。formatter等の自動修正が必要ならworker側で`harnesslint --fix`を実行し、その後checkを通す。

installer/managed-file behaviorを変更した場合のoffline install smokeは`glm-worker --install-smoke --role <role>`で実行する。証拠cacheや再利用layerは持たず、必要なときに実行結果そのものを証拠とする。

## fail closed

- 昇格境界で実行したfull suiteの失敗は実装不具合として扱い、sandbox由来と推測してskip・成功扱いしない。
- 新しいcapability不足を一次証拠で確認した場合だけ固定入口へ最小追加する。汎用command実行formや直接`go test`をallowするprefix ruleは追加しない。
- 同一snapshotの同一gateを環境選択だけを理由に重複実行しない。
