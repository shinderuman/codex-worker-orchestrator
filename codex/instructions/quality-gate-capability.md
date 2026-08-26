# capability必要quality gateの実行境界

親Codexが自身の検証・final gateでquality gate commandを直接実行する場合だけ適用する。GLM worker/reviewerはglm-worker配下のClaude Code sessionでcommandを実行しCodex sandboxを通らないため対象外であり、glm-workerの実行契約は`~/.codex/instructions/glm-execution.md`のまま変更しない。

## 既知capabilityと実行境界の対応

- full-suite Go test（`glm-worker` module）: 一次証拠でUnix socket bindを必要とするのは、現在このrepoの`glm-worker` moduleのfull suite（glm-worker停止endpoint等のapp系test）だけである。このsuiteはsandbox内では`listen unix ...: bind: operation not permitted`で成立しないため、`glm-worker` module rootから`glm-worker --quality-gate go-test`・`glm-worker --quality-gate go-test-race`を実行し、既存の`glm-worker` prefix ruleによって最初から昇格境界（sandbox外）へ一度だけdispatchする。
- 他moduleのGo suite（`tools/merge-json`等）・`go vet ./...`・`go build`・gofmt・commentlint・`git diff`等: sandbox内で成立するため直接sandbox実行のまま最小権限を維持する。capability根拠のないcommandやmoduleを昇格させない。
- working tree内のscriptや`go test`自体へprefix ruleを直接追加しない。prefix ruleはargv前方一致のため直接`go test`をallowすると後続flagで任意commandを昇格できる（`go test ./... -exec /bin/sh`等）。実行権限の境界はinstalled glm-worker binaryだけが持つ。

## 固定実行境界の契約

- この入口は`go test ./...`・`go test -race ./...`の固定argvだけを実行する。formの選択以外に引数を受け付けず、余分なargvは受理せずusage errorでfail closedする（`go test ./... -exec /bin/sh`相当の昇格経路を作らない）。拒否はconfig読込・state・go process起動より前に完了する。
- 子process環境の`GOFLAGS`は空に固定し、環境変数経由のflag注入も同一の固定argv保証へ折り込む。実行dirは呼出時のcurrent directory（module root）であり、入口側でrepoを選び直さない。
- machine出力は成功時stdoutのJSON object 1件だけである。失敗時はstdoutへ出力せず、stderrのstructured process error JSON（`kind:"quality_gate_failed"`）と非zero exitで伝える。subprocess出力はlog fileへ保存し、結果JSONの`log`に保存先pathだけを載せる。full install smoke証拠の取得・再利用は`~/.codex/instructions/install-smoke-evidence.md`の単一入口が担ったままであり、この入口はGo suite gate専用として併存する。

## 決定論的dispatch契約

- 既知capabilityを必要とするgate commandは、sandbox内で一度失敗させてから同じ全suiteをsandbox外で再実行しない。最初から上記入口で一度だけ実行し、「失敗1回＋再実行1回」を「有効な実行1回」に収束させる。
- 対象mapping内のmoduleでrouted form以外の形式（直接`go test`・flag付きvariant等）をsandbox内で実行して同じsignatureに失敗した場合はrouting missである。同一gateを上記入口で再実行して成立を確認し、それでも失敗する場合は実装不具合として扱う。
- 新たなcapability不足を一次証拠で把握したgateだけ、対象module mappingへの追加と固定formの追加としてこの入口へ最小実装する。将来別moduleで同一capability不足を一次証拠で確認するまでは、対象は`glm-worker` moduleのfull suiteのみとする。全部のcommandやmoduleの無条件昇格、汎用command実行form、Unix socket等をhardcodeする汎用sandbox frameworkは作らない。管理rules fileへ直接`go test`等のcommandをallowするruleを追加しない。

## 環境失敗と実不具合の受理集合

- 環境失敗として受理するのは、sandbox実行で観測されたUnix socket bind拒否の既知signature（`listen unix ...: bind: operation not permitted`）と、それが同一package内へ波及した二次失敗だけである。
- 昇格境界で実行したgateの失敗は、すべて実装不具合としてfail closedに扱う。sandbox由来と推測してskip・成功扱い・acceptance緩化を行わない。環境失敗の認定に失敗分類の推測を使わない。
- 同一snapshotの同一gate証拠を、環境選択だけを理由にsandbox内外で二重取得しない。昇格境界での1回の実行結果を証拠とする。
