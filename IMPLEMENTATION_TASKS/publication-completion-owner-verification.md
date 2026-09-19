# Task: publication completion owner verification

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- External Reviewでcompletion handover owner verificationが`CurrentTaskAuthorityPath()` lookup errorを無視し、canonical task authorityを取得できない状態でもowner比較を継続し得ることを確認した
- wrong lifecycle ownerを成功側へ縮退させる可能性はpublication sequence本体とは独立したcompletion authority correctnessである
- formal Dogfood最終failureではreopen lineageとcompletion provenanceが衝突したが、その問題は別Taskで扱い、本taskはcanonical owner lookup/verificationのfail-closed性だけを所有する

## Purpose

publication completionでcanonical task authorityの取得・owner照合をfail closedにし、wrong ownerまたはauthority不明状態でcompletionを成功扱いしない。

## External feasibility

status: not-applicable

## Contract

- completion handover owner verificationは`CurrentTaskAuthorityPath()`等のcanonical lookupが成功した場合だけlifecycle taskと比較する
- lookup error、missing/ambiguous authority、task identity mismatchはmachine-visible failureとしてcompletionを拒否する
- authority path文字列だけでなく既存task identity/snapshot bindingを再利用し、第二のowner stateを作らない
- legitimate current lifecycle ownerは既存complete flowへ進める
- escaped-defect reopen後のlineage/provenance semanticsは`publication-escaped-defect-reopen-lifecycle.md`へ委ね、本taskで特殊case bypassを追加しない

## Must not

- authority lookup errorを空文字列/noneとして比較継続しない
- mismatchをwarningだけで成功扱いにしない
- reopenを通すためにwrong-owner fail-closed invariantを弱めない
- Plan/Task fileを親の推測で探索してownerを補完しない

## Acceptance criteria

- canonical authority lookup error fixtureでcompletionがfail closedする
- missing/ambiguous/wrong lifecycle owner fixtureが成功しない
- exact owner fixtureは既存completion flowを維持する
- reopen lineage fixtureは本taskのowner verificationを迂回せず、別lineage authorityから正しいownerが供給される前提で成立する
- relevant completion tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- completion ownerはcanonical tracked task authorityとruntime lifecycle identityの一致で証明する
- authority不明は成功ではない

## Dependencies

none
