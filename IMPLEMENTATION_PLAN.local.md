# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/external-feasibility-dispatch-gate.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/015-fixed-eval-corpus.md`
- `IMPLEMENTATION_TASKS/009-worker-call-outliers.md`
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
- implementation baseline: checkout/state隔離実装・完了metadata同期・本配置済みのcurrent HEAD
- implementation boundary: Greptile要求をCodex scheduled task＋既存git/Greptile CLIだけへ縮小。日次automation `greptile-review`はremote `refs/heads/codex/greptile-reviewed..refs/heads/main`をreview範囲の唯一の正とし、repository production source変更なし
- preserved boundary: external feasibility gateの中断diffはmessage identity付きstash 2件へ可逆保全し、orphan process group終了済み
- push: GLM push、force/non-fast-forward、他refへのpushは禁止。Greptile運用に必要な`refs/heads/main`と`refs/heads/codex/greptile-reviewed`の親Codexによる通常fast-forwardだけ許可

## 現在の停止理由

external feasibility gateの中断diffはmessage identity付きstash 2件へ保全済み。Greptile日次reviewはremote checkpoint..remote mainだけを対象にし、local-only commitとdirty working treeはreview対象外とする。dirty working tree自体は許容する。

## 次の親Codex操作

保全済みexternal feasibility gateのstash identity・適用順・現在task stateを再照合し、checkout隔離導入後の安全な復元経路で同taskを再開する。pushはGreptile運用のremote main/checkpoint通常fast-forward以外禁止。
