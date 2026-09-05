# Task: External review follow-up for a70d35c..43e1da9

## Original instruction

````text
EXTERNAL_REVIEW_INTAKE

range: a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82..43e1da969a4118122e65c09863794028a0ddb139

gpt_review:\
status: PROPOSAL\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/347\
branch: gpt-review/a70d35c-43e1da9\
proposal_head: 63055a10727ca63e620b244560cf0b34c0010f0a

external_review:\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/346\
base_sha: a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82\
head_sha: 43e1da969a4118122e65c09863794028a0ddb139\
coderabbit: READY\
greptile: READY

Codex:

- current authorityを再確認する
- GPT proposalがあればfetchしてproposal_headを確認する
- External review PRのrangeを確認する
- CodeRabbit / Greptile双方のreview完了を確認する
- GPT / CodeRabbit / Greptileの全findingをlosslessにGLMへ渡す
- 同じsubstantive reviewをGLM前に再実行しない

GLM:

- current HEAD / Rules / relevant task contractに対して全findingを検証する
- 成立する問題を修正し必要なtestを実行する

transport failure、range不一致、外部review未完了時はGLMへ進まない。
````

## Amendments

none

## Resolved references

- 2026-09-05に親Codexが`origin/gpt-review/a70d35c-43e1da9`、`origin/pr/346`、`origin/pr/347`を明示fetchした
- current HEADとPR 346 headは`43e1da969a4118122e65c09863794028a0ddb139`、baseは`a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82`で一致し、baseはheadのancestorでrangeはnon-emptyだった
- GPT proposal branchとPR 347 headはいずれも`63055a10727ca63e620b244560cf0b34c0010f0a`だった。proposal diffはこのimmutable SHAをlocal参照して検証し、blind mergeしない
- CodeRabbitは`43e1da969a4118122e65c09863794028a0ddb139`に対するreviewを`2026-09-04T15:34:29Z`に完了し、完了通知は`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#issuecomment-5542667799`、reviewは`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#pullrequestreview-5114991553`
- Greptileはlatest reviewed commitを`43e1da969a4118122e65c09863794028a0ddb139`と明示し、Confidence Score 5/5、`No concrete changed-code defect remains`と報告した。原文は`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#issuecomment-5542714510`
- GPT proposal本文のfinding原文:
  - `Preserve non-available token-delta status for subsequent parent turns instead of reporting false available.`
  - `Treat missing input/cached endpoint counters as unknown instead of silently emitting zero deltas.`
  - `Includes regression tests for counter-reset propagation and partial endpoint anchors.`
  - `Do not merge blindly; downstream GLM must revalidate findings against current authority.`
- CodeRabbit actionable finding 1、Minor / Stability & Availability、`codex/instructions/glm-stop-isolate.md:25-26`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527435`:
  - `Keep the isolation branch until resume verification completes.`
  - `verifyIsolationIntegration resolves record.Branch with state.ResolveBranchTip before checking whether its tip is integrated. If external cleanup deletes the branch, --resume fails closed even when the integrated commit remains in the current history. Retain the branch until the original task completes, or persist and verify an immutable tip SHA.`
- CodeRabbit actionable finding 2、Major / Functional Correctness、`codex/instructions/quality-gate-capability.md:21`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527447`:
  - `Check repository containment before EvalSymlinks.`
  - `When EvalSymlinks(evidence.WorkingDir) fails, finalizationVerifiedEvidenceDir returns ("", nil). finalizationRoutingDecision then validates callerDir, so a missing or broken-symlink path outside the repository can trigger caller-cwd fallback. Perform lexical containment checking first and return routing_evidence_outside_repository for outside paths.`
- CodeRabbit actionable finding 3、Minor / Functional Correctness、`glm-worker/internal/app/execute_test.go:549`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527454`:
  - `Create valid scheduler fixtures for each identity-mixup case.`
  - `Each subtest writes only automation.toml. The missing scheduler row can produce VerificationError with autoresume.Fail even if target-thread validation is removed or broken. Write a matching scheduler row for the automation key used by each command, including the key derived from the wrong wake thread ID. Then the test will reach the identity comparison.`
- CodeRabbit actionable finding 4、Major / Security & Privacy、`glm-worker/internal/app/install_smoke.go:75`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527465`:
  - `Redact all authorization values.`
  - `Line 75 only consumes an optional Bearer scheme. Authorization: Basic dXNlcjpwYXNz leaves the Base64 credential in the evidence file. Authorization=Bearer <token> does not match at all. Redact the complete value for both separators, and add regression cases for Basic and equals-form values.`
  - 提案regexpは`(?i)\\b(authorization\\s*[=:]\\s*)[^\\r\\n]*`だが、そのまま採用せずcurrent codeで境界と過剰redactionを検証する
- CodeRabbit actionable finding 5、Minor / Functional Correctness、`glm-worker/internal/app/install_smoke.go:227`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527502`:
  - `Keep five evidence runs in total.`
  - `The current run is excluded at Lines 215-218. This condition then retains five older runs, so the directory grows to six runs. Retain only four prior entries when retainedInstallSmokeEvidenceRuns is five, and update TestInstallSmokeEvidenceRetentionKeepsRecentRuns to expect five total runs.`
- CodeRabbit actionable finding 6、Major / Data Integrity & Integration、`glm-worker/internal/app/parent_handoff.go:325`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527510`:
  - `Reject routing evidence when taskID is empty.`
  - `ReadOr("task.id", "") returns an empty ID when task.id is missing or blank. With a repository and snapshot present, latestRoutingEvidenceRuns accepts a passing record whose omitted TaskID also decodes as empty. The handoff can therefore emit taskless legacy evidence. Reject empty taskID before scanning, and add a regression case without task.id.`
- CodeRabbit actionable finding 7、Minor / Functional Correctness、`glm-worker/internal/app/timeline.go:185-186`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527522`:
  - `Do not report complete coverage after skipped event records.`
  - `At Line 185, a readable event log always produces "complete". readTaskEventRecords can skip malformed records without returning an error. The output then omits records but reports complete coverage. Pass skipped into coverage evaluation and report partial coverage when it is nonzero. Add this assertion to TestTimelineSkipsCorruptLines.`
- CodeRabbit actionable finding 8、Major / Functional Correctness、`IMPLEMENTATION_RULES.md:49`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#discussion_r3935527531`:
  - `Define the writer for parent-maintenance metadata.`
  - `IMPLEMENTATION_RULES.md:38-49 permits the parent Codex to edit Rules/Plan/Task metadata directly, but codex/AGENTS.md:44-46 requires explicit authorization for the exact direct edit. glm-parent-action provides no metadata-edit command. Add a scoped parent-maintenance exception to codex/AGENTS.md, or require and record exact direct-edit authorization before applying this rule.`
- CodeRabbit outside-diff finding、`glm-worker/internal/app/app.go:220`、同review URL:
  - `Add --verify-codex-wake to the top-level usage text.`
  - `Line 220 omits the new command. A user who invokes glm-worker without arguments cannot discover --verify-codex-wake from the usage output. Add its arguments to this usage string and assert the output in a parser test.`
- CodeRabbit pre-merge observation、`https://github.com/shinderuman/codex-worker-orchestrator/pull/346#issuecomment-5542662309`:
  - `Docstring Coverage`はwarning、0.00%、required threshold 80.00%、changed scopeの198 functionsを解析し21 unsupportedと報告した。repositoryの既存policy・要求に照らして成立性を判断し、warningだけを理由に無関係な大量docstring追加を行わない
- Greptileのfinding結果原文:
  - `No concrete changed-code defect remains, although the PR description explicitly says this external-review PR must not be merged.`
  - `The investigated authorization, evidence-retention, telemetry-boundary, and parent-usage paths retain concrete parser, sanitization, identity, repository-boundary, and interval-partitioning safeguards, with no reachable observable failure established.`

## Purpose

固定rangeに対するGPT proposal、CodeRabbit、Greptileの結果をcurrent authorityへ再検証し、成立するdefectだけを修正して回帰testを追加する。

## External feasibility

status: verified

## Contract

- ACTIVE化時のcurrent HEAD、current `IMPLEMENTATION_RULES.md`、relevant task contractに対し、Resolved referencesの全findingとobservationをGLM workerが個別に検証する
- GPT proposalはlocalのimmutable `63055a10727ca63e620b244560cf0b34c0010f0a`をdiff sourceとして参照し、成立する変更だけをcurrent HEADへ適応する
- CodeRabbitのinline 8件、outside-diff 1件、pre-merge observation 1件、GPT 2 finding、Greptileのno-finding結論をすべてdispositionし、成立・既修正・非成立・out-of-scopeを根拠付きで区別する
- 成立する問題を最小scopeで修正し、各原因境界を通る回帰testを実行する
- GLM処理後は既存の独立reviewer、Sol semantic review、current snapshot validation、accept/install flowへ戻る

## Must not

- GPT proposal branchまたはExternal review PRをblind mergeしない
- GPT / CodeRabbit / Greptileと同じsubstantive reviewをGLM前に親Codexが再実行しない
- transport failure、range不一致、review未完了を推測で補わない
- CodeRabbitの提案diff、bot向けprompt、docstring warningを検証なしで実装しない
- Greptileのno-finding結論を他reviewer findingの棄却根拠にしない

## Acceptance criteria

- 全findingに一意なdispositionとcurrent source locatorがある
- 成立した各defectに、findingが示すfalse-pass / false-complete / secret exposure / routing failure等を実際に防ぐ回帰testがある
- GPT proposal由来変更はproposal SHAとの差分とcurrent HEADへの適応内容を追跡できる
- relevant targeted testsとrepository quality gateが成功する
- independent GLM reviewer、Sol semantic acceptance、commit、必要なinstall/smoke、通常のfast-forward pushを完了する

## Historical invariants

- External review用PR 346とGPT proposal PR 347はreview transportであり、merge対象ではない
- GLM worker/reviewerにGit remote write authorityを与えない

## Dependencies

none

## Review findings

- Resolved referencesにreview intake時点の全findingを固定済み。ACTIVE化後にcurrent authorityへdispositionする

## Current boundary

Plan priorityを正とし、先行するblocking recovery taskの完了後にACTIVE化する。
