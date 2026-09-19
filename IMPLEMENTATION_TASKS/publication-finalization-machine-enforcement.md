# Task: publication finalization machine enforcement

## Original instruction

````text
それはなんでなの？バグなの？手順を間違えたの？手順を間違えたなら手順を間違えること自体がバグなんじゃないの？なんで起票しないの？
````

## Amendments

none

## Resolved references

- parent acceptance後のcanonical handoffは `complete` と `install` だけを提示し、publication candidate作成・readiness・promotion・push bindingの正規sequenceをaction specとして提示しなかった
- dirty treeで `glm-parent-action complete` を実行すると `tree_not_clean` で停止したが、次に必要なmachine-admitted actionを返さなかったため、親Codexが通常の `git commit` を選択できる状態だった
- 通常commit後の再実行は `publication_candidate_missing` で停止し、正規flowへ戻る非破壊的なrecovery actionもcanonical handoffに提示されなかった
- ref更新を保護する `.githooks/reference-transaction` はrepositoryでnon-executable modeとしてtrackedされ、`core.hooksPath=.githooks` の状態でもGitから無視された
- formal Dogfoodの最初のpublication candidateではcanonical sequenceが`git push origin <candidate-oid>:<remote-ref>`を返したが、managed pre-push guardはlocal refが`refs/heads/*`でありexact candidate OIDを指すことを要求したため、machineが生成した正規command自身がguardに拒否された
- user-authorized bounded fixではpush sourceをpromoted local branch refへ変更し、managed pre-push guardを通ってremote syncまで成功した。したがってcanonical action specはcandidate OIDの手動再構成ではなく、candidateへpromote済みのlocal branch refをsourceにする必要がある
- 以前本taskへ統合したExternal Review hardening（completion owner lookup / Git guard shell semantics / promotion atomicity / managed hook install integrity）はformal Dogfoodでtask肥大化・非収束を招いたため、独立Taskへ再分割する

## Purpose

parent acceptance後のpublication finalizationをmachine-admittedな一本道として強制し、親Codexが順序を記憶していなくてもcandidate未作成commitやguard不在のref更新へ進めないようにする。

## External feasibility

status: not-applicable

## Contract

- acceptance後のcanonical handoffは、current stateに応じてpublication candidate作成、必要なinstall/readiness、promotion、remote binding、push、completionのうち合法な次操作だけをexact action specで返す
- dirty treeや未作成candidateを単なる終端failureにせず、正規sequenceへ進むためのmachine-admitted next actionまたはbounded recovery actionを返す
- awaiting parent completion中のbranch ref更新とremote pushは、repository setupに依存して黙って無効化されないguardでfail closedにする
- hookを使用する場合はtracked mode、install/setup後の実効性、`core.hooksPath`との整合をmachine testで保証する。ただしmanaged hook ownership/install migrationの詳細責務は`managed-publication-hook-install-integrity.md`をownerとする
- candidateを作らずcommitされた状態を検出した場合、親によるstate file手編集や手順推測を要求せず、安全性と履歴影響を明示したbounded recoveryを提供する
- parent action、Git hook、completion runtime間でpublication authorityとtask identityを一意に保つ
- candidate promotion後のcanonical pushは、managed pre-push guardが観測するlocal ref identityと整合するpromoted local branch refをsourceにし、remote tracking refへexact action specでpushする
- parentにcandidate OID / local branch / remote refからpush refspecを手動再構成させない
- publication Git guard shell classification、promotion mutation atomicity、completion owner lookup、escaped-defect reopen lineageはそれぞれ独立Taskをownerとし、本taskへ再吸収しない

## Must not

- publication sequenceを親Codexの記憶、自由文packet、手動command再構成へ依存させない
- guard failureやhook未実行を警告だけで成功扱いにしない
- recoveryのためにpublication state fileを親Codexへ直接編集させない
- GLM worker/reviewerへGit remote write authorityを与えない
- remote sync、review、required validation、runtime installの既存gateをskipしない
- candidate object OIDをsource refspecへ直接使い、managed pre-push guardとの契約不整合を再導入しない
- 独立review/rollback可能なpublication hardening責務を再びumbrellaとして吸収しない

## Acceptance criteria

- clean baselineからreview PASS後、canonical handoffだけを辿ってcandidate作成からcompletionまで到達するintegration scenarioが成功する
- dirty treeでの早すぎる `complete` が、通常commitを推測させずexactな次actionを返すtestがある
- candidate未作成のbranch ref更新が確実に拒否され、hook/setupがnon-executableまたは不整合なら事前検証で判別可能に失敗する
- candidate準備前にcommit済みとなったfixtureから、machine-declared recoveryだけで安全に正規状態へ復帰できる
- local branch、remote OID、publication candidate、task stateの不一致をそれぞれfail closedで固定する
- promoted candidateのpush action specが`refs/heads/<local>:<remote-ref>`相当のlocal branch sourceを使い、実managed pre-push hookを通過してremoteへexact candidateを同期するintegration fixtureがある
- object-OID source pushがmanaged pre-push guardに拒否されても、親の手動command修正を要求せずcanonical projection自体が正しいbranch-ref actionを返す
- 既存の正規publication flow、parent-only remote write、install/validation gateを維持する

## Historical invariants

- final HEAD closureとremote同期はcurrent completion runtimeを正とし、親CodexがGit状態から推測しない
- GLM worker/reviewerにGit remote write authorityを付与しない
- parent actionの合法な次操作とtransportはcanonical handoffのaction specsを正とする
- independent publication hardening responsibilityは個別Task/commit境界を維持する

## Dependencies

- `IMPLEMENTATION_TASKS/managed-publication-hook-install-integrity.md`
- `IMPLEMENTATION_TASKS/detached-candidate-runtime-vcs-identity.md`
- `IMPLEMENTATION_TASKS/publication-git-guard-shell-semantics.md`
- `IMPLEMENTATION_TASKS/publication-promotion-atomicity.md`
- `IMPLEMENTATION_TASKS/publication-completion-owner-verification.md`
