# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/commentlint-sandbox-safe-launcher.md`

## NEXT（優先順）
- `IMPLEMENTATION_TASKS/021-conditional-improvements.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
- `IMPLEMENTATION_TASKS/autonomous-development-harness.md`
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
- accepted main baseline before F1-F10 hardening integration: `815efc8f6601d344a93e8fa213d0227306db1f0d`
- implementation boundary: PR #3 `6f08b301e9c4e1444af80c9b9ee646eb778a3baa`以降のGPT hardeningでrepository quality enforcement、instruction/authority guard、review/search boundary等を大幅に強化し、F1-F10とF9のhard dependency 016まで完了した。F1 `instruction-surface-ownership`はPR #8 `621a6c6cec384b8ef0488796b6488085a77a6b5d`、F2 `unknown-production-surface-risk`はPR #12 `f33203418c33d85baee34fa69225ef954e7996aa`、F3 `glm-git-authority-enforcement`はPR #13 `627e0dcfa15148a68684c3ff9008a4658c5a2615`、F4 `quality-evidence-mutation-risk`はPR #14 `87af3f4700f5dd8220c582efa62f18f561c33c20`、F5 `deterministic-rule-activation`はPR #15 `9c33ae4784b6cfe33cf63de8d415fac38cad9fae`、F6 `sol-decision-boundary-enforcement`はPR #16 `16d975e02dc37b2279cce441d09bebc85adef3f3`、F7 `prose-semantic-guard-migration`はPR #17 `1b4546548a5089c8160f6f53263ea91fb39a821d`、F8 `instruction-conflict-resolution`はPR #18 `407595e2e2a8a095c16a93624bef57d1b2bb32b3`、016 `worker-repo-search-integration`はPR #22 `da468541683a832b102acecc60770678452e6fa4`、F9 `reviewer-diff-first-search`はPR #19 `948ea31ee4381f4192afeff607c015696349a9a1`、F10 `exhaustive-search-gate`はPR #20 `6633569d2e30acd62470de74b3966bd824cef1af`としてintegration済み
- preserved boundary: wake coalescing、machine output、safe-stop/resume、provider accounting、parent-managed metadata guard、GLM commit/push禁止、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持
- current implementation: post-hardening local runtime recovery、安全なCLI help、protected repository単位Git authority guardをcurrent mainへ統合済み。Task 010はcurrent mainで評価集計・full test・独立reviewを再確認し、既存責務粒度基準と現行resume境界の維持、hard cap・強制事前分割・強制semantic milestone不採用を決定して完了。Task 011は10値閉集合のoperation categoryをraw command・path本文なしでevent metadataへ保存し、旧eventを`other`へ畳み込むstate層の単一read-only集計まで実装・review・Sol採否・commitを完了。必要性の証拠がないtimeline JSON拡張はSol reviewで撤回済み。Task 012は保存済みeventのcompaction boundary集計、baseline、deterministic workflow evidence、3面のsemantic regression transport固定scenarioを実装し、独立review・Sol採否・commitを完了。現証拠ではthreshold変更はNo-Go。Task 013は既存ModelCallLog v3とRoundRecordだけを使うper-repository model routing評価を実装し、independent review・Sol採否・full validationを完了。現dataはsingle resolved modelのためquality delta unknownでrouting変更はNo-Go。Task 014は既存event/telemetry/roundだけを読むper-repository test impact評価を実装し、test call数・duration・failure outcomeを可視化、suite-level coverageをunknown、omission candidateを空として保持し、independent review・Sol採否・full validation・commitを完了。Task 019はdefault-on repo-search flag、read-only CLI、managed instruction、installer wiringを実装し、worker/reviewer通常BM25だけを切替対象としてdiff navigationとexhaustive proofを常時維持した。instruction-surface guard recovery整合taskでは、復元済みafter-call mutationをtyped guard-recoverable checkpointへ収束し、pending decisionを除去して保存済みresultまたはcheckpointから汚染sessionを再利用せず同一taskをresetなしで独立review terminalまで継続可能にした。Task 020はBM25 worker/reviewer routeのquery category・outcome・result count・durationをraw query本文なしでtask statsへexact-once加法記録し、保存済みevent/statsだけを読む`--repo-search-eval`と既存A/B reportのoptional `repo_search` blockへ接続した。exhaustive-search query persistence reviewでは、query identityにproduction consumerがないことを確認し、新規proof eventのraw query永続化だけを停止して既存proof・legacy decode・append-only eventを維持した。Codex analysis evidence bundleは明示parent identity、parent/Guardian rollout、bounded log/process projection、allowlist runtime settingを既存bundle v3へ統合し、独立review・Sol採否・full test/race/vet/build/lintを完了した
- merge boundary: hardening task PRはintegrationへSquash Mergeしてtask単位commitを作り、最終integration→mainはPR #21で非Squash mergeして各task commit履歴を保持する

## 現在の停止理由

Codex analysis evidence bundle taskを完了した。次のACTIVE `IMPLEMENTATION_TASKS/commentlint-sandbox-safe-launcher.md`は未着手で停止している。

## 次の親Codex操作

次回はrepository authorityとACTIVE task本文を再読し、dependencyと開始条件を確認してcommentlint sandbox taskを通常workflowで開始する。Task 021は同じrunで開始しない。
