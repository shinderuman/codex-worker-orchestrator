# Task: Parent Git push completion binding

## Original instruction

````text
Push失敗もバグなんじゃないのか
````

## Amendments

none

## Resolved references

- 2026-09-08、local `main` のcommit `f9c4c14` と`1cc47a1`を`origin/main`へ通常pushする親操作が外部安全審査で拒否された
- 拒否理由は、対象branch・remoteへのユーザー明示承認が確認できない状態でdefault branchへ2 commitを外部writeするriskだった
- 拒否後のGit現物はlocal `main`が`origin/main`より2 commit aheadで、実装・metadataはlocalに保持されている。外部安全審査の拒否を迂回せず、remote同期済みとも扱わない

## Purpose

親completion flowがGit remote writeの権限・実行・拒否・再試行境界を曖昧にせず、local完了をremote同期済みと誤認したり、後続taskへ未通知のahead commitを累積したりしないようにする。

## External feasibility

status: implementation

assumption: Codex appの外部安全審査に対し、repository側からauthority不足とpush後postconditionを機械判定できる境界、および再利用可能なremote write authorization scopeが存在する
evidence-source: producer
evidence: 2026-09-08の実Codex app安全審査はorigin/mainへのpushを明示承認不足として拒否し、2026-09-08のread-only実remote照会ではrefs/heads/main=304cd16301d7f84a3362191403f3c82a605c2d4b、local HEAD=d864873b5cf1921089d926456010fa52d911304eで1 commit aheadだった。PoCはrepository側でexact remote/ref・ahead/behind・last outcome・ls-remoteによるpostconditionを判定可能だが、Codex app側の再利用可能なtarget-bound authorization scopeは観測・制御できないと確認した
go: 2026-09-08 Sol High判断。repository側のpreflight・remote-sync-pending分類・postconditionを実装へ進め、Codex app認可は外部責務としてpositive判定せず、明示的なtarget-bound authorizationがない場合はpush前にexact remote/ref付きでユーザー判断へ戻す

## Contract

- まずPoCで、親Codex・Codex app safety review・Git remote write・repository stateの責務境界を一次証拠から確定する
- 通常completionがpushを要求する場合、remote/refと必要なauthorizationを実行前に一意にし、不足時は外部writeを試行する前にexact target付きでユーザー判断へ戻す
- push拒否・network failure・non-fast-forward・remote postcondition不一致をlocal task完了やremote同期成功へ縮退しない
- local commit、ahead/behind、対象remote/ref、last push outcomeをbounded machine evidenceとして次の正規actionへ渡す
- 成立する場合だけ、既存parent action / project state / completion postconditionへ最小統合し、別daemon・DBを追加しない

## Must not

- 外部安全審査を迂回・弱体化しない
- メッセージや過去の一般依頼からGit remote write権限を推測しない
- GLM worker/reviewerへGit remote write authorityを与えない
- `main`、`origin`、default branchを暗黙固定しない
- push失敗後に別command、別transport、force pushで同じwriteを迂回しない

## Acceptance criteria

- 現在の拒否を再現可能なfixtureまたはbounded evidenceで、failure分類・remote/ref・authorization state・local ahead commitを一意に示す
- authorization不足はpush前に停止し、exact remote/refを伴うユーザー判断入口を返す
- push成功時はexpected local commitがexpected remote refへ到達したpostconditionを確認する
- rejection、network failure、non-fast-forward、remote ref mismatch、local clean but aheadをremote-sync-pendingとして区別するtestがある
- external boundaryだけで解決不能ならPoC No-Goとして、必要なCodex app側変更とrepository側で保持できる最小状態を分離して報告する
- independent reviewer、Sol Go/No-Go、必要なvalidation、commit/install/smokeを完了する

## Historical invariants

- GLM worker/reviewerへGit remote write authorityを与えない
- external safety reviewの拒否をrepository instructionで無効化しない

## Dependencies

none

## Review findings

none

## Current boundary

現在ACTIVE taskを中断しない。完了後にPoCとしてACTIVE化し、local `main`がremoteよりaheadの現物を保持したまま責務境界を確認する。
