# Task: detached candidate runtime VCS identity

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- failed publication Dogfoodではcandidate commitをdetached linked worktreeから実`install.sh`したとき、installed `glm-worker`のVCS/build identityがcandidate commitへ確実に束縛されることがpublication correctnessの前提になった
- current ACTIVE umbrellaへこのruntime identity責務まで吸収され、publication sequence / hook ownership等と同じreview/fix loopへ載った結果、独立rollback/review境界が失われた
- `unko` branchのfailed implementationはforensic evidenceとして保持するが、そのdiff自体を実装authorityとして再利用しない

## Purpose

publication candidateをdetached linked worktreeからinstallするproduction経路で、installed runtime identityがexact candidate commitへ束縛されることを独立したintegration責務として固定する。

## External feasibility

status: not-applicable

## Contract

- detached linked worktreeから実`install.sh`したinstalled `glm-worker` / companion runtimeのVCS identityは、source repositoryの別branchやouter worktreeではなくexact candidate commitを表す
- candidate source、installed runtime build identity、publication candidate OIDをmachine-readableに比較できる既存authorityを再利用する
- producer-realistic fixtureは実際のdetached linked worktree + installer経路を通し、test-only metadata注入で成立させない
- source treeにdirty/untracked差分がある場合、その差分をcandidate commit identityへ誤帰属しない
- normal attached installの既存identity semanticsを維持する

## Must not

- detached HEADを理由にVCS identityをunknown成功へ縮退させない
- outer repository HEADや呼出元worktreeのbranch identityをcandidate identityとして流用しない
- publication lifecycle、hook ownership、promotion atomicity等の別責務を本taskへ吸収しない
- test fixtureだけの環境変数でproduction identity contractを代替しない

## Acceptance criteria

- producer-realistic detached linked worktreeからcandidate commitをinstallし、installed runtimeがexact candidate OIDを報告するintegration fixtureがある
- outer/attached worktreeが別HEADでもcandidate identityへ誤混入しない
- dirty/untracked差分をcandidate commit identityとして成功扱いしない
- normal attached installのidentity regressionがない
- relevant install/runtime tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- runtime build/source identityはpublication candidateと比較可能なmachine evidenceである
- installed runtimeをcandidateとして検証する際、親CodexのGit推測をauthorityにしない

## Dependencies

none
