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

status: poc

unknown:

- Codex appの外部安全審査へrepository側から渡せる明示authority fieldと、repository内で機械化可能な境界
- default branch pushのユーザー承認をtask単位・branch単位・継続workflow単位のどこまで再利用できるか

go_criteria:

- 安全審査を迂回せず、push前のauthority不足とpush後のremote同期postconditionを親workflowから機械判定できる
- 拒否時にlocal commitと対象remote/refを保持したremote-sync-pending状態または同等の一意な再開入口を構成できる

no_go_criteria:

- repository側に観測・制御可能な境界がなく、外部製品変更または都度のユーザー承認だけが唯一の成立手段である

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
