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

## Purpose

parent acceptance後のpublication finalizationをmachine-admittedな一本道として強制し、親Codexが順序を記憶していなくてもcandidate未作成commitやguard不在のref更新へ進めないようにする。

## External feasibility

status: not-applicable

## Contract

- acceptance後のcanonical handoffは、current stateに応じてpublication candidate作成、必要なinstall/readiness、promotion、remote binding、push、completionのうち合法な次操作だけをexact action specで返す
- dirty treeや未作成candidateを単なる終端failureにせず、正規sequenceへ進むためのmachine-admitted next actionまたはbounded recovery actionを返す
- awaiting parent completion中のbranch ref更新とremote pushは、repository setupに依存して黙って無効化されないguardでfail closedにする
- hookを使用する場合はtracked mode、install/setup後の実効性、`core.hooksPath`との整合をmachine testで保証する
- candidateを作らずcommitされた状態を検出した場合、親によるstate file手編集や手順推測を要求せず、安全性と履歴影響を明示したbounded recoveryを提供する
- parent action、Git hook、completion runtime間でpublication authorityとtask identityを一意に保つ

## Must not

- publication sequenceを親Codexの記憶、自由文packet、手動command再構成へ依存させない
- guard failureやhook未実行を警告だけで成功扱いにしない
- recoveryのためにpublication state fileを親Codexへ直接編集させない
- GLM worker/reviewerへGit remote write authorityを与えない
- remote sync、review、required validation、runtime installの既存gateをskipしない

## Acceptance criteria

- clean baselineからreview PASS後、canonical handoffだけを辿ってcandidate作成からcompletionまで到達するintegration scenarioが成功する
- dirty treeでの早すぎる `complete` が、通常commitを推測させずexactな次actionを返すtestがある
- candidate未作成のbranch ref更新が確実に拒否され、hook/setupがnon-executableまたは不整合なら事前検証で判別可能に失敗する
- candidate準備前にcommit済みとなったfixtureから、machine-declared recoveryだけで安全に正規状態へ復帰できる
- local branch、remote OID、publication candidate、task stateの不一致をそれぞれfail closedで固定する
- 既存の正規publication flow、parent-only remote write、install/validation gateを維持する

## Historical invariants

- final HEAD closureとremote同期はcurrent completion runtimeを正とし、親CodexがGit状態から推測しない
- GLM worker/reviewerにGit remote write authorityを付与しない
- parent actionの合法な次操作とtransportはcanonical handoffのaction specsを正とする

## Dependencies

none
