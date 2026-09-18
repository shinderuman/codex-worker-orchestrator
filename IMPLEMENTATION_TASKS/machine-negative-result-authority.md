# Task: machine negative result authority

## Original instruction

````text
43に残っているやつを全部IMPLEMENTATION_PLANに戻せ
順序もちゃんと考慮してIMPLEMENTATION_PLANにしろ
もちろんFindingで見つかったやつも全部IMPLEMENTATION_PLANにする
````

## Amendments

none

## Resolved references

- Dogfood bundle `3de7a4cd-b296-4630-a3e1-7acd342b9c0a` ではrequired install Gateがmachine negative resultを返した後、parentが既知NEXT原因・別責務・allowed actions等を理由にpublicationへ進めた
- concrete publication事故はcurrent treeでmandatory publication Gateとして修正済みだが、machine-enforced control全般について「negative resultをparent semantic judgmentだけでsuccessへ昇格できない」という共通authority contractは別途必要である
- machine enforcementのlayer legitimacy自体は先行task `machine-enforced-control-authority-legitimacy.md` で再確認する

## Purpose

正当にmachine-enforcedと分類されたcontrolのnegative admission/resultを、parent独自解釈で成功へ読み替える経路を共通authority contractとしてfail closedにする。

## External feasibility

status: not-applicable

## Contract

- canonical control classificationから、machine-enforced controlのnegative admission/resultに対する共通authority boundaryを導出する
- `reject / blocked / failed / not-admitted`等のnegative resultは、同じmachine ownerが定義する明示的override/recovery transitionなしにparent semantic judgmentだけでsuccess/admittedへ昇格できない
- `partial` / `semantic-parent-only` controlに残るresidual parent judgmentは維持する
- machine-enforced controlでも、machineがadmitした複数action候補からparentがsemanticに選択する権限は維持する
- controlごとの「このfailureは無視禁止」というブラックリストを増やさず、classification / projection / admissionのsingle authorityから一貫して導出する
-追加model callを導入しない

## Must not

- すべてのmachine outputをparentが再評価不能とする過剰な権限制限にしない
- error文字列やcommand名の列挙で実装しない
- current treeのmandatory publication Gateを重複実装しない
- semantic decision authorityをmachineへ移さない

## Acceptance criteria

- representativeなmachine-enforced negative resultをparent理由だけでsuccessへ昇格できない回帰がある
- explicit machine-owned recovery/overrideがある場合だけ正規transitionとして進める
- partial / semantic-parent-only controlの既存parent judgmentが維持される
- admitted action間のsemantic選択が維持される
- classification / projection / admissionのsingle authorityからdriftなく導出される
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- machine factをparent都合で成功へ読み替えない
- machine authorityの正当性そのものは別taskで先に確定する

## Dependencies

- `IMPLEMENTATION_TASKS/machine-enforced-control-authority-legitimacy.md`
