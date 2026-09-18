# Task: parent fix origin and cause staged transport

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- Dogfoodではparentが2回のfix payload内で`origin: glm-reviewer` / `cause: cross-cutting-invariant`をsemanticに確定した
- machine projectionのnext commandは`glm-parent-action fix <token>`だけで`--origin/--cause`をtransportせず、TaskStats / telemetryではorigin/causeが`unknown`になった
- runtime保存側は既にorigin/causeを受理できるため、normal staged transportの欠損である

## Purpose

parentが確定したfix origin/causeをstaged action transportでlosslessにruntimeへ渡し、TaskStats / telemetryへ同じ値を保存する。

## External feasibility

status: not-applicable

## Contract

- staged fixのmachine-owned action specがparentの確定したorigin/causeをlosslessにtransportできるcanonical slot/parameterを持つ
- parentaction facadeとunderlying fix commandの既存validation vocabularyを再利用する
- payload本文にmetadata風の自由文を書かせることをmachine telemetry contractにしない
- origin/causeを指定しない合法ケースはunknown/unspecifiedとして明示的に扱い、推測しない
- one-shot staged payload、stdin hash/bytes、accepted-scope/approval-only等の既存transport safetyを維持する

## Must not

- telemetry側でpayload自然言語をparseしてorigin/causeを推測しない
- origin/causeのためだけに別model callを追加しない
- semantic origin/cause分類をGLMへ移さない

## Acceptance criteria

- staged fixでvalid origin/causeを指定するとexecuted actionとTaskStats/telemetryへ同じ値が記録される
- `glm-reviewer` / `cross-cutting-invariant`相当fixtureが`unknown`へ落ちない
- invalid/unsupported metadataはpayload消費前またはcanonical validation boundaryでfail closedする
- metadata未指定の既存fix path、accepted-scope、approval-only、one-shot consume semanticsが維持される
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- semantic metadataをtransport途中で欠落させない
- metadata値をmachineが自然言語から推測しない

## Dependencies

none
