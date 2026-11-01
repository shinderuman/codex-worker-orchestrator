# Task: worker sandboxへ実Docker socketを渡さない正規test実行境界

## Original instruction

````text
Docker test backend契約が有効なworker sessionで、実Docker daemon socketをworker sandboxへ渡さない。runner/guard lifecycleに載る単一操作executor（設計はdocker-test-owner-isolation-design.md）で正規Go test実行を代行し、任意host bind mount・任意Docker API・protected Git/host書込・remote write authorityをworkerへ渡さない。新恒常daemon・汎用Docker proxyを追加しない。設計承認をAmendmentsへ確定してから実装する。
````

## Amendments

### 2026-11-01 親設計判断

runner/guard lifecycle内の単一操作executor案を採用する。実Docker socket・任意Docker API・host bind mountをworkerへ渡さない。既存guardの期間だけ稼働し、新恒常daemonや汎用proxyを作らない。protocolはschema_version/token/argsのみ、実行rootはowner固定、Docker引数をrequestから構築しない。go testのflag値（-run regexp等）を壊す独自の先頭文字制約は入れず、固定go test配列へのshellを介さない引数渡しと、host権限に関係するfield不在で境界を守る。

file stagingではrequest/responseの最大size・締切・取消・cleanup・単一実行・token再利用拒否を持つ。worker書込可能なpathをowner権限で追従しないよう、symlink/特殊file/親directory置換/TOCTOUとresponse上書きを拒否する。ownerの実行用一時領域・logはworkerの自由なpath指定を受けない。staging responseの偽造をcanonical品質gate成功の証拠へ昇格させない。既存のwrapper直実行gateは変更しない。

Dockerfile/sourceはuntrusted入力として扱い、固定のhost-mountなし実行境界を保つ。workerからDocker CLIとraw socketを直接利用できない負case、通常の./goquality test診断が実ownerを経由する正caseを確認する。Bash deny追加だけを修正として扱わない。--C proxy修正は同じtest入口制約を成立させる変更として同Taskに含め、native実行迂回を拒否する。

## Resolved references

- CR-1409-3 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1409#discussion_r4397725555 · `glm-worker/internal/runner/git_authority_sandbox.go:21-24`付近
  原文: "** Major · Security & Privacy · Heavy lift** **Docker socket access lets the worker escape the filesystem sandbox.** When `dockerPolicy` is active, the worker's Bash sandbox can connect to the Docker daemon socket. The daemon runs outside the sandbox. A worker command such as `docker run -v /:/host ...` can then read or write any host path. This includes the repository `.git` metadata that `denyWrite` protects. It also includes paths outside `allowWrite`. The post-run `verify()` check only detects changes to HEAD, refs, index, and local config. It does not prevent the writes. It does not detect changes to other host paths. The current Bash deny rules block only direct `go test` forms. They do not restrict `docker` subcommands. Choose one of these mitigations: - Add narrow deny rules for `docker run`, `docker exec`, `docker cp`, `docker build`, and `docker compose`. Allow only the `goquality` entry point to use the socket. - Expose a restricted socket proxy that permits only the calls `goquality` needs. - Document this as an accepted trust boundary in `codex/instructions/worker/go.md`."
- GR-1441-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1441#discussion_r4434912055（同一指摘の統合）
  原文: "P1 Security **Docker bypasses protected Git writes** On Darwin ARM64, enabling the Docker backend gives the worker direct access to the Docker daemon socket. The worker can still run Docker commands through `Bash`. It can start a container that mounts the repository and writes protected `.git` files through the daemon, outside the shell's write restrictions. Keep the socket outside the worker sandbox and route test requests through a restricted owner instead. **How this was verified:** The worker receives the daemon endpoint through `DOCKER_HOST`, the sandbox permits that socket, and the worker's command restrictions do not block Docker operations."
- 併記（同一file群・独立修正）: CR-1409-4 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1409#discussion_r4397725572 · `glm-worker/internal/runner/go_test_proxy.go:20`付近
  原文: "**Handle both spellings of the `-C` flag.** The Go command accepts `--C dir` as well as `-C dir`. The proxy recognizes only `-C` as a value-taking flag. Therefore, `go --C dir test ./...` can be forwarded to the real Go executable instead of being denied. Add `--C` to the value-taking case."
- 設計案: `/Users/shinderumanm/.glm-worker/sessions/a25858d98a1498ee2096ef42d60ff4dff9b5dc5e8c3c02301a55d34f1b9859e3/artifacts/716e1f06-9150-4b76-b83d-5638ad5c538c/docker-test-owner-isolation-design.md`（実装前に親判断。Bash denyのみの案は親により却下済み）。

## Purpose

workerに任意Docker操作能力を与えずに、Docker backend契約どおりの正規Go test診断を実行できるようにする。

## External feasibility

status: not-applicable

## Contract

- 親承認済み設計に従い、worker環境から実socket（`DOCKER_HOST` exportとsandbox `allowUnixSockets`）を除去し、guard/runner lifecycleに載る単一操作executor経路でDocker実行を代行する。
- request/source/argument binding: 実行rootはexecutor側でsessionに固定されたlane rootのみ。requestはroot・mount・image・entrypointを受け付けず、未知fieldはfail-closed拒否。argsは`go test` pass-through形式のみ。
- staging通信は既存guard tempDir配下のworker書込可能領域を使い、unix socketをworker sandboxへ1つも許可しない。
- 正規品質gate・parent_validationのwrapper直実行経路は変更しない。Mac native迂回・検証省略をしない。
- `go_test_proxy`は`--C`（および`-C=`/`--C=`形式）をvalue-taking扱いし、`go --C dir test`をdenyする。

## Must not

- 新恒常daemon・汎用Docker API proxy・第二sandbox設定を追加しない。Runtime binding・権限の捏造をしない。`codex/instructions/worker/go.md`の保証記述を実態と乖離させない（実装と同時に正しい内容へ更新する調整は親指示による）。

## Acceptance criteria

- 設計doc第4節の拒否test 1–6（socket不在拒否・未知field拒否・args形式拒否・並行request拒否・response欠落fail-closed・wrapper直実行回帰）が自動testで成立すること。
- dockerPolicy有効sessionでworker起因の`docker` CLI・raw socket接続が機械記録とともに拒否されること。
- `go --C <dir> test`がproxy denyになる回帰test。

## Historical invariants

- Docker利用不能時も入口は失敗となり迂回しない。機械証跡行（`goquality-linux-diagnostic`等）規約を維持する。

## Dependencies

none
