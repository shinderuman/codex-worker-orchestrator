# Task: publication escaped-defect reopen lifecycle

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- 2026-09-19のACTIVE task Amendmentで、`awaiting-parent-completion`後にescaped correctness defectがold accept/candidateをinvalidにした場合、同一Taskを正規fix/review lifecycleへ戻す恒久machine transitionを追加する要求が保存された
- formal Dogfoodでは複数回のcanonical `reopen`自体は許可済みcontractに沿っていたため、回数だけを欠陥とは判定しない
- reopen後candidateの`BaseHead`がすでに旧Task file削除・Plan handover済みcommitになった一方、completion verifierはpublication baseに旧lifecycle Taskがtrackedされていることを要求し、push成功後に`completion_transition_invalid: lifecycle task ... was not tracked at publication base`でcomplete不能になった
- `TaskStatusPtr`の`awaiting-parent-completion` projection、reopen後のwaiting-sol-review/fix routingもfailed implementationで必要性が確認された
- 最初のone-time temporary reopen手順、および最終push defect限定のuser-authorized reopenは例外運用の証拠であり、そのtemporary mechanism自体を恒久contractにはしない

## Purpose

escaped correctness defectでaccepted publicationをinvalidateして同一Taskへ正規reopenした後も、task provenance / publication lineage / completion owner authorityをlosslessに維持し、wrong-owner fail-closedを弱めず再review→candidate→push→completeまで収束できるようにする。

## External feasibility

status: not-applicable

## Contract

- `awaiting-parent-completion`からescaped defectを理由にreopenするmachine transitionは、old accept/candidateをinvalid化し、同じcanonical Task identityをwaiting-sol-review/fix lifecycleへ戻す
- reopen transitionはorigin/causeを既存typed vocabularyで記録し、ad-hoc state file編集やtemporary overlayを通常経路にしない
- reopen後に新candidateを作成する際、completion provenanceは単純なlatest `BaseHead`存在確認に退化せず、reopen前から継続するcanonical task authority/lineageを証明できる
- reopen後candidate baseですでに旧Task fileが削除・Plan handover済みでも、legitimate same-task lineageはcomplete可能である
- wrong task / unrelated candidate / forged lineageは従来どおりfail closedする
- `awaiting-parent-completion` handoff/recovery projectionはtask statusをnon-nullで返し、reopen/fixの合法actionをcanonical action specとして提示する
- successful reopen round後は通常のreview/validation/install/publication gatesを再度満たし、新candidateでremote sync後にcompleteする

## Must not

- reopen回数だけを上限にしてescaped correctness defectを隠さない
- completionを通すために「baseにtask fileがなくても常にOK」とする等、wrong-owner verificationを無効化しない
- old candidate/old acceptを有効なまま新fixを重ねない
- one-time temporary Go overlayや手動state surgeryを恒久normal pathにしない
- reopenを理由に別Task responsibilityをcurrent Taskへ無制限吸収しない

## Acceptance criteria

- awaiting-parent-completionからescaped correctness defectをreopenし、waiting-sol-review→fix→review→acceptへ正規遷移できる
- reopen前publication commitでTask file削除/Plan handover済み、そのcommitを新candidate baseにするfixtureでもcanonical lineageを保持してpush→completeまで成功する
- unrelated/wrong task candidateは同fixtureでもcompletionを拒否する
- reopen後handoff/recoveryがnon-null task statusとexact allowed/required actionを返す
- old candidate/acceptが再利用されず、新snapshotのrequired validation/install/publication evidenceが必要になる
- relevant lifecycle/publication integration tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- semantic Task identityはpublication commit上のTask file存在だけではなくcanonical lifecycle authorityで維持される
- escaped defect修正後もcompletion owner correctnessを弱めない
- user-authorized temporary exceptionを恒久machine contractと混同しない

## Dependencies

- `IMPLEMENTATION_TASKS/publication-finalization-machine-enforcement.md`
- `IMPLEMENTATION_TASKS/publication-completion-owner-verification.md`
