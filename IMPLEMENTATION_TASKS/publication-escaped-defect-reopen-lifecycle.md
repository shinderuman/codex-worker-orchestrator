# Task: publication escaped-defect reopen lifecycle

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

### 2026-09-20 — reopen authority boundary

- Codex / parentが判断するのは「correctness findingが成立した」というsemantic judgmentまでとする。
- `awaiting-parent-completion`でcurrent accepted publicationをinvalidateするdurable correctness findingが成立した後のlifecycle遷移はmachine-ownedとする。
- invalidating findingなしでは`required_action = complete`、invalidating findingありでは`required_action = reopen`をmachineが一意に投影する。
- `allowed_actions = [complete, reopen]`のようにCodexへlifecycle選択を戻してはならない。
- reopen authorityはparent指定のorigin/causeだけでは成立せず、current TaskID・対象accepted snapshot/candidate・finding identity/disposition等のdurable machine evidenceへbindingする。

## Resolved references

- 2026-09-19のACTIVE task Amendmentで、`awaiting-parent-completion`後にescaped correctness defectがold accept/candidateをinvalidにした場合、同一Taskを正規fix/review lifecycleへ戻す恒久machine transitionを追加する要求が保存された
- formal Dogfoodでは複数回のcanonical reopen自体は許可済みcontractに沿っていたため、回数だけを欠陥とは判定しない
- reopen後candidateの`BaseHead`がすでに旧Task file削除・Plan handover済みcommitになった一方、completion verifierはpublication baseに旧lifecycle Taskがtrackedされていることを要求し、push成功後に`completion_transition_invalid: lifecycle task ... was not tracked at publication base`でcomplete不能になった
- `TaskStatusPtr`の`awaiting-parent-completion` projection、reopen後のwaiting-sol-review/fix routingもfailed implementationで必要性が確認された
- 最初のone-time temporary reopen手順、および最終push defect限定のuser-authorized reopenは例外運用の証拠であり、そのtemporary mechanism自体を恒久contractにはしない

## Purpose

escaped correctness defectがcurrent accepted publicationをinvalidateしたとmachine evidenceで確定した場合だけ、machine-required reopenとして同一Taskを正規fix/review lifecycleへ戻す。task provenance / publication lineage / completion owner authorityをlosslessに維持し、wrong-owner fail-closedを弱めずfresh review→validation→install→candidate→publication→completeまで収束できるようにする。

## External feasibility

status: not-applicable

## Contract

- `awaiting-parent-completion`ではmachineがcurrent accepted snapshot/candidateに対するdurable invalidating correctness findingの有無を評価する
- invalidating findingが存在しない場合は`required_action = complete`とし、通常のcompletion pathを維持する
- invalidating findingが存在する場合は`required_action = reopen`とし、`complete`をadmitしない。Codexへ`complete`と`reopen`の選択肢を並べない
- invalidating finding evidenceは少なくともcurrent TaskID、対象accepted snapshot/candidate identity、finding identity、machine-visible dispositionをbindingし、forged/stale/unrelated evidenceをfail closedする
- reopen authorityはorigin/cause文字列だけでは成立しない。origin/causeは必要ならprovenance metadataとして記録できるが、reopen admissionのauthority sourceにはしない
- machine-required reopenはold accept、old candidate、およびそれらへbindingされたvalidation/install/publication evidenceをinvalidateし、同じcanonical Task identityをwaiting-sol-review/fix lifecycleへ戻す
- reopen後はfresh review、required validation、runtime install、candidate creation、publication evidenceをすべて再要求する。old snapshot evidenceを再利用しない
- reopen回数そのものに上限を設けない。reopen後に新しいcurrent accepted publicationをinvalidateする別のdurable correctness findingが成立すれば再度machine-required reopenできる
- reopen後に新candidateを作成する際、completion provenanceは単純なlatest `BaseHead`存在確認に退化せず、reopen前から継続するcanonical Task authority/lineageを追加証明として扱う
- reopen後candidate baseですでに旧Task fileが削除・Plan handover済みでも、legitimate same-task lineageはcomplete可能である
- wrong Task / unrelated candidate / forged or stale finding / target accepted snapshotと無関係なfinding / forged lineageはfail closedする
- `TaskStatusPtr` / handoff / recovery projectionは`awaiting-parent-completion`をnon-nullで返し、machineが決定したexact `required_action`だけを正確に投影する

## Must not

- `allowed_actions = [complete, reopen]`等としてCodex / parentへlifecycle判断を戻さない
- Codexが任意に「念のためreopen」を発行できる汎用actionを作らない
- parent指定のorigin/cause文字列だけをreopen authorityにしない
- durable invalidating findingなしにreopenをadmitしない
- invalidating findingが成立済みなのにcompleteをadmitしない
- reopen回数だけを上限にしてescaped correctness defectを隠さない
- completionを通すために「baseにtask fileがなくても常にOK」とする等、wrong-owner verificationを無効化しない
- old candidate / old accept / old validation-install-publication evidenceを有効なまま新fixを重ねない
- one-time temporary Go overlayや手動state surgeryを恒久normal pathにしない
- reopenを理由に別Task responsibilityをcurrent Taskへ無制限吸収しない

## Acceptance criteria

- `awaiting-parent-completion`でinvalidating correctness findingがないfixtureは`required_action = complete`を返し、reopenを選択肢として提示しない
- current accepted snapshot/candidateへbindingされたdurable invalidating correctness findingが成立したfixtureは`required_action = reopen`を返し、completeをadmitしない
- machine-required reopen後、同一Task identityでwaiting-sol-review→fix→review→acceptへ正規遷移できる
- forged/stale finding、wrong TaskID、unrelated candidate、対象accepted snapshot/candidateと一致しないfindingではreopenをadmitせずfail closedする
- reopen前publication commitでTask file削除/Plan handover済み、そのcommitを新candidate baseにするfixtureでもcanonical lineageを保持してpush→completeまで成功する
- unrelated/wrong task candidateは同fixtureでもcompletionを拒否する
- reopen後handoff/recoveryがnon-null task statusとmachine-decided exact required actionを返す
- old candidate/acceptおよび旧snapshotに紐づくvalidation/install/publication evidenceが再利用されず、新snapshotのrequired evidenceが必要になる
- reopen後の新accepted publicationに対して別のdurable invalidating findingが成立すれば、回数上限なしで再度machine-required reopenできる
- relevant lifecycle/publication/finding integration tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- semantic Task identityはpublication commit上のTask file存在だけではなくcanonical lifecycle authorityで維持される
- escaped defect修正後もcompletion owner correctnessを弱めない
- user-authorized temporary exceptionを恒久machine contractと混同しない
- semantic correctness judgmentとlifecycle transition authorityを分離し、後者はdurable machine evidenceで決定する

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/publication-completion-owner-verification.md`
- `IMPLEMENTATION_TASKS/publication-finalization-machine-enforcement.md`
