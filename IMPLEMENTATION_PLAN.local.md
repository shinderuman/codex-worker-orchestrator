# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/machine-output-boundary-enforcement.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`
- `IMPLEMENTATION_TASKS/010-task-splitting-milestones.md`
- `IMPLEMENTATION_TASKS/011-operation-category-telemetry.md`
- `IMPLEMENTATION_TASKS/012-compaction-threshold-evaluation.md`
- `IMPLEMENTATION_TASKS/013-worker-model-routing-evaluation.md`
- `IMPLEMENTATION_TASKS/014-test-impact-evaluation.md`
- `IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md`
- `IMPLEMENTATION_TASKS/017-reviewer-diff-first-search.md`
- `IMPLEMENTATION_TASKS/018-exhaustive-search-gate.md`
- `IMPLEMENTATION_TASKS/019-repo-search-product-wiring.md`
- `IMPLEMENTATION_TASKS/020-repo-search-telemetry-eval.md`
- `IMPLEMENTATION_TASKS/021-conditional-improvements.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/desktop-terminal-payload-double-render-boundary.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`

## 現在のGit境界

- branch: `main`
- implementation baseline: 同一snapshotのfull install smoke PASS証拠再利用をcurrent HEADへ収録した状態
- implementation boundary: source/smoke input/toolchain環境の全identity一致だけでStateStore配下のPASS ledgerを共有し、worker→reviewer→fix→reviewer→parentの代表loop実実行を5回から2回へ削減。変更・異環境・失敗・破損はfail closedで再取得し、parent管理metadata-onlyとcommit前後は同一identityを維持する。`--install-smoke`は成功stdout単一JSON、失敗structured process error + non-zeroへ収束済み
- preserved boundary: 43 installer scenario・production install preflight・全quality gate・反復コスト観測を維持し、外部machine output共通boundaryの機械強制をACTIVEへ昇格。force/non-fast-forward・tag・他ref・他repository・GLM commit/push禁止を維持
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

なし。single-shot/streamの外部machine output contractを共通boundaryとdeterministic gateで強制する境界。

## 次の親Codex操作

ACTIVE taskのlossless requirementを正として、全command出力経路・single-shot/stream分類・stdout/stderr ownershipをGLMへ監査・設計・実装させる。
