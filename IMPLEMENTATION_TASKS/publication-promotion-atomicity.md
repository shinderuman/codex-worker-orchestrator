# Task: publication promotion atomicity

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- External Reviewでpublication promotionがbranch refをcandidateへ進めた後にsource/cleanliness post-checkへ失敗し、`blocked` resultとadvanced branch stateが共存し得ることを確認した
- failed publication umbrellaへこのref mutation atomicity責務まで吸収されたため、canonical sequence本体と分離する

## Purpose

publication promotionを、blocked resultとbranch ref mutationが矛盾しないatomic transitionとして固定する。

## External feasibility

status: not-applicable

## Contract

- candidate promotionでbranch refを進める前後の検証・mutation順序を、failure時に外部観測可能stateが矛盾しないよう設計する
- ref advance後に必須postconditionが失敗する可能性を残す場合は、安全なrollbackまたは同等のtransactional recoveryを持つ
- `blocked` / failureを返した時点で、成功扱いできないcandidate ref advanceを残さない
- concurrent/mutation fixtureでref変更とsource/cleanliness検証のraceを固定する
- existing candidate identity、reference-transaction guard、remote push boundaryを維持する

## Must not

- advanced refを残したままerror文字列だけ返してfail closedとみなさない
- rollback failureを黙って無視しない
- remote pushやcompletion lifecycleまで本taskへ拡張しない
- normal successful promotionで余分なparent/model round tripを追加しない

## Acceptance criteria

- branch ref advance後にpost-check failureを注入するfixtureで、blocked resultとadvanced refが共存しない
- concurrency/mutation fixtureでcandidate/source mismatchをfail closedに保つ
- successful promotionはexact candidateへ一度だけrefを進める
- rollback/reordered transitionでreference-transaction guard等の既存safetyを弱めない
- relevant promotion tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- publication transitionのmachine resultとGit refの実stateは矛盾してはならない
- remote publicationはlocal promotion成功後の別境界である

## Dependencies

none
