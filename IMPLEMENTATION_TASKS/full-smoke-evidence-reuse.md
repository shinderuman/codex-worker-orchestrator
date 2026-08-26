# Task: 同一snapshotのfull smoke PASSをfeedback loopで再利用する

## Original instruction

````text
TASK

`cdf96471175fea4003943064a21a51bb0e104d54` で install smoke 1回あたりの重複実行は削減したが、feedback loop内で同じ品質証拠を何度も再取得する問題が残っている。
**同一の有効なsnapshotについて取得済みのfull smoke PASSを再利用し、worker → reviewer → fix → reviewer → parent gate間の不要な再実行を止める。**

CURRENT STATE

- install smokeは43 installer scenarioを維持。
- real `go test ./...` は44回→3回。
- full smokeは1418秒→約302〜306秒。
- 現在の「反復コスト観測」は反復を検出・報告・task化判断する仕組みであり、同一品質証拠の再取得自体は防止していない。
- そのため同じ変更に対してworker/reviewer/parentがそれぞれfull smokeを再実行し、5分単位の重複が発生し得る。

DO

- 現在のworkflowでfull smokeがどのround・gateから実行されるか一次証拠で棚卸しする。
- 「同じ品質証拠を再取得する必要がある条件」と「既存PASSを再利用できる条件」を明確化する。
- 同一のsmoke-relevant snapshot/environmentに対する成功証拠は、後続roundで再利用可能にする。
- 最低限、証拠から以下を機械的に照合できるようにする。
  - 対象source / relevant working-tree identity
  - `tests/install_smoke.sh` と `install.sh` を含むsmoke-relevant input identity
  - 必要なtoolchain/environment identity
  - result
  - 完了時点
- 既存task artifact / state / telemetry等で十分なら再利用し、新しいcache subsystemを作らない。
- reviewerがsourceを変更していない場合、同一snapshotのfull smoke PASSを理由なく再実行しない。
- fix等でsmoke-relevant inputが変わった場合は旧証拠を無効化する。
- 修正途中は必要なtargeted verificationを使い、最終candidateについて必要なfull smokeを取得する。
- metadata-only等、smoke semanticへ影響しない変更まで無条件に証拠無効化しない。
- failure後、環境変化、installer/smoke/test contract変更等では必要な再実行を維持する。

DO NOT

- test・quality gate・acceptance criteriaを削減しない。
- 「以前PASSした」という自然言語だけで再利用しない。
- timestampだけで有効性を判断しない。
- snapshotが変わっているのに古いPASSを使わない。
- generic build cache、CI system、巨大なevidence frameworkを新設しない。
- full smokeの5分をさらに短くすることを本taskの主目的にしない。
- worker/reviewerへ「なるべく再実行するな」というpromptだけを追加して完了扱いにしない。

ACCEPTANCE

- 典型的な

  `worker → reviewer → fix → reviewer → parent final gate`

  の各境界について、full smoke実行/再利用条件が決定論的に説明・検証できる。
- 同一有効snapshotのPASSを複数roundが共有でき、同じfull smokeを機械的に再取得しない。
- sourceまたはsmoke-relevant input変更時には旧証拠が確実に失効する。
- unrelated metadata-only変更で不要に失効させない。
- stale/異環境/失敗証拠をPASSとして再利用できない。
- 改善前後で、代表的feedback loopにおけるfull smoke実実行回数を測定する。
- 品質coverageを落としていない根拠を残す。
- 関連test、全必要gate、独立review、必要なSol品質gateを通す。

EXECUTION

- 現在のACTIVE taskは中断しない。
- 本件を独立taskとしてPlanへ保持し、適切な処理境界で実施する。
- `cdf9647` の「反復コスト観測」機構を置き換えるのではなく、**既知の反復については観測後に同じ浪費を続けない実行機構**として補完する。
````

## Amendments

none

## Resolved references

- `cdf96471175fea4003943064a21a51bb0e104d54`はinstall smoke内部のreal Go suite重複を削減した完了commitである。
- 「現在のACTIVE task」はtask受領時の`IMPLEMENTATION_TASKS/comment-lint-empty-line-fix.md`を指し、本task追加だけを理由に中断・切替しない。
- feedback loopはworker、独立reviewer、fix/re-review、親Codex final gateの通常lifecycleを指す。

## External feasibility

status: not-applicable

## Purpose

full smokeのquality coverageを維持したまま、同一のsmoke-relevant snapshotとenvironmentに対するmachine-verified PASS証拠を後続roundで共有し、同じ5分級実行の再取得を止める。

## Contract

- full smokeの全実行入口とround/gate ownershipを一次証拠で棚卸しし、実行・再利用・失効条件を決定論的に定義する。
- PASS証拠はsource/relevant working-tree、installer/smoke/test contract、toolchain/environment、result、完了時点をmachine identityで照合できる場合だけ再利用する。
- reviewerがsourceを変更していない同一snapshotでは既存PASSを共有し、fixやsmoke-relevant input変更、環境変化、failureでは再取得する。
- parent-managed metadata-only変更はsmoke semanticsへ無関係な場合に限り不要な失効を起こさない。
- 既存artifact/state/telemetryで成立するかを先に確認し、新しいcache subsystemを作らない。
- 途中roundはtargeted verification、最終candidateは必要なfull smokeというcoverage責務を維持する。

## Must not

- test、quality gate、acceptance criteriaを削減しない。
- 自然言語PASS、timestampだけ、変更後snapshotへ古い証拠を流用しない。
- generic build cache、CI、巨大evidence framework、prompt依頼だけの抑止を追加しない。
- full smoke本体の再高速化や`cdf9647`の反復コスト観測置換へscopeを広げない。

## Acceptance criteria

- worker→reviewer→fix→reviewer→parent gateの実行/再利用条件がmachine testで固定される。
- 同一有効snapshotのPASSは1回だけ実取得し後続roundが共有する。
- source/smoke-relevant input/環境/failure/staleで失効し、unrelated metadata-only変更では不要に失効しない。
- stale、異環境、failure証拠をPASSへ昇格できない。
- 代表loopの改善前後実行回数とcoverage維持根拠を記録する。
- 関連test、通常quality gate、独立review、必要なSol gate、親commit・remote main fast-forward、本配置を完了する。

## Historical invariants

- install smokeは43 installer scenarioとproduction install preflightを維持する。
- `cdf9647`の反復コスト観測と親のtask化判断は継続する。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。comment lint fixを中断せず完了させた後に昇格し、full smokeの実行入口・証拠identity・再利用/失効条件を一次証拠から設計する。
