# Task: GLM containment拒否理由をactionableにする

## Original instruction

````text
あと諸々の機構がGLMを囲う檻にはなってるけどなんでコマンドが止められてるのかわからずそこで無駄に迷走している感があるのでコマンドが止められてる理由というか、なんで使えないのかをGLMに示してやるようにすれば実装速度が上がったりするんじゃないかとおもった
````

## Amendments

````text
履歴が見つからなかった場合の話だがAcceptanceに限らず、どういう状態のファイルなのかわからないため全体的にそのまま作業するべきではないと思う
前段としてそのタスクのGo/No-Goを決めてから作業するべきだと思う
````

````text
Deniai改善は先にやったほうがいいんじゃ？
これも観測必要だろ？
````

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

## Resolved references

- 実dogfood GLM transcriptでは`ps`の`operation not permitted`後に`/bin/ps`を再試行、Go home cache、Unix socket、git-guard等でも拒否境界の切り分けに追加turnを使っていた。
- repository-owned git authority guardは現在、blocked subcommandをattempt logへ記録してexit 97するが、worker側へ返るstderr自体は説明を持たず、attempt log書込自体が拒否された場合は真のguard reasonが隠れる。
- OS/Claude runtime sandbox等、repositoryがauthoritatively理由を説明できない拒否も存在するため、対象はrepository-owned boundaryに限定する。

## External feasibility

status: not-applicable

## Purpose

檻を弱めず、repository-owned containmentが拒否した瞬間にowner/reason/既知の安全な次行動をcompactかつdeterministicに返し、GLMが同値commandを試し直す無駄を減らす。

## Contract

- 実ログで観測したdenialをowner別に分類する。
- repository-owned guard/wrapperだけをauthoritativeに説明する。
- denial説明は常時promptへ追加せず、拒否が発生したtool result/stderrまたはtyped machine resultで返す。
- stable category/reasonを用い、raw secret/path/policy internalsを不要に露出しない。
- 安全な代替が機械的に一意な場合だけ、compactなnext actionを含める。
- guard自身のdiagnostic side effect失敗が本来の拒否理由を上書きしない設計にする。

## Must not

- external sandbox failureをrepository policyと断定しない。
- denyをallowへ変えない。
- model-based policy interpreterを追加しない。
- rare failureのために大きなper-turn prompt taxを追加しない。

## Acceptance criteria

- representative repository-owned blocked Git mutation/transportが、generic exitだけでなくstable owner/reasonをGLMへ返す。
- diagnostic記録先へ書けない場合でも本来のdeny reasonがstderr/resultから失われない。
- 既知のsafe alternativeがあるcovered caseでは、そのbounded alternativeを返せる。
- current guard invariants・Git mutation prevention・testsを維持する。
- 次のcommentlint dogfood bundleでcovered denial発生時の同値再試行数を評価できる。

## Historical invariants

- GLM commit/push禁止、parent-managed metadata guard、repository Git authority guardを維持する。
- unknown external denialはunknownのままにする。

## Dependencies

- `IMPLEMENTATION_TASKS/unscheduled-task-state-reconciliation.md`

## Review findings

none

## Current boundary

repository-owned Git mutation/transport denialのproduction実装はPR #173 `4a27a709a1cf0bb6b766623daea5aff61fab51b3`で統合済み。blocked Git mutation/transportは`kind=containment_denial`・`owner=git-authority-guard`・stable reason・bounded `next_action`をstderrへ返し、exit 97とcontainment自体は維持する。`blocked-attempts`記録はbest-effortへ下げ、記録先へ書けなくてもauthoritative deny reasonを失わない。generic OS/Claude sandbox denialはrepository policyとして分類しない。残るAcceptanceは次のcommentlint dogfood bundleでcovered denialの同値再試行数を実測すること。
