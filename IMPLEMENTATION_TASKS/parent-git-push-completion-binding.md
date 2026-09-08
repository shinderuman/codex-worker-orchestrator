# Task: Parent Git push completion binding

## Original instruction

````text
Push失敗もバグなんじゃないのか
````

## Amendments

### 2026-09-08

````text
Pushはいつするの？
どうしてPushしてない状態が放置されてるの？バグじゃないの？
````

### 2026-09-08（false-complete判明後）

````text
え、それじゃあめちゃくちゃおかしなバグをうみだしてないか？？？？
なにかPushに関する不具合解消のタスクやっていたよな？お前なにしてくれてるの？？
````

## Resolved references

- 2026-09-08、local `main` のcommit `f9c4c14` と`1cc47a1`を`origin/main`へ通常pushする親操作が外部安全審査で拒否された
- 拒否理由は、対象branch・remoteへのユーザー明示承認が確認できない状態でdefault branchへ2 commitを外部writeするriskだった
- 初回実装で`glm-parent-action push-binding`はexact remote/ref・authorization state・remote postconditionを返すようになった
- 初回完了直後、local `main`が`origin/main`より3 commit aheadのままtask metadata同期とsession rotationへ進んだ。新sessionで手動実行した`push-binding`は`authorization:user_decision_required`を返しており、判定commandの存在だけでは通常completion lifecycleを拘束できていない
- 2026-09-08、親Codexがcurrent HEAD `4d8bab88b4ad59f3483cd98422ad09fe4cac553e`を`origin/refs/heads/main`へ通常pushし、installed `push-binding --expected-oid ... --attempt-outcome completed`が`classification:synced`、`postcondition.met:true`を返した。即時のremote差異は解消したが、親が手動で気付くことへ依存するlifecycle gapは未解消である

## Purpose

親completion flowがGit remote writeの権限・実行・拒否・再試行境界を曖昧にせず、local完了をremote同期済みと誤認したり、後続task・session rotationへ未通知のahead commitを累積したりしないようにする。

## External feasibility

status: implementation

assumption: Codex appの外部安全審査に対し、repository側からauthority不足とpush後postconditionを機械判定できる境界、および再利用可能なremote write authorization scopeが存在する
evidence-source: producer
evidence: 2026-09-08の実Codex app安全審査はorigin/mainへのpushを明示承認不足として拒否した。repository側ではexact remote/ref・ahead/behind・last outcome・ls-remoteによるpostconditionを判定可能だが、Codex app側のauthorization自体は外部責務である。初回実装後の実completionで、手動`push-binding`を呼ばないままaccept・task同期・rotationへ進める残存gapを確認した
go: 2026-09-08 Sol High判断。repository側のpreflight・remote-sync-pending分類・postconditionに加え、通常completionとrotationのadmissionを機械bindingする。Codex app認可は外部責務としてpositive判定せず、明示的なtarget-bound authorizationがない場合はpush前かつtask advance前にexact remote/ref付きでユーザー判断へ戻す

## Contract

- 通常completionがpushを要求する場合、remote/refと必要なauthorizationを実行前に一意にし、不足時は外部writeを試行する前にexact target付きでユーザー判断へ戻す
- remote-sync-pendingが未解消の間は、task metadata同期、次task開始、session rotation完了のいずれも成功扱いにしない
- push拒否・network failure・non-fast-forward・remote postcondition不一致をlocal task完了やremote同期成功へ縮退しない
- local commit、ahead/behind、対象remote/ref、last push outcomeをbounded machine evidenceとして次の正規actionへ渡す
- 既存parent action / project state / completion postconditionへ最小統合し、別daemon・DBを追加しない

## Must not

- 外部安全審査を迂回・弱体化しない
- メッセージや過去の一般依頼からGit remote write権限を推測しない
- GLM worker/reviewerへGit remote write authorityを与えない
- `main`、`origin`、default branchを暗黙固定しない
- push失敗後に別command、別transport、force pushで同じwriteを迂回しない
- `push-binding`を親が任意に呼ぶ手順説明だけで再完了扱いにしない

## Acceptance criteria

- authorization不足はpush前かつtask advance前に停止し、exact remote/refを伴うユーザー判断入口を返す
- remote-sync-pendingのまま`accept`後metadata同期・次task開始・session rotation完了を試みる代表scenarioがfail closedする
- push成功時はexpected local commitがexpected remote refへ到達したpostconditionを確認する
- rejection、network failure、non-fast-forward、remote ref mismatch、local clean but aheadをremote-sync-pendingとして区別するtestがある
- independent reviewer、Sol semantic review、必要なvalidation、commit/install/smokeを完了する

## Historical invariants

- GLM worker/reviewerへGit remote write authorityを与えない
- external safety reviewの拒否をrepository instructionで無効化しない

## Dependencies

none

## Review findings

- 初回実装はread-only分類commandを追加したが、通常completionからの強制呼出とtask/rotation admissionを拘束せず、親がcommandを呼び忘れるthreat modelを満たしていなかった

## Current boundary

false-completeとして直ちにACTIVE再開する。remote-sync-pendingを通常completion・task advance・session rotationの機械postconditionへ統合し、このtask完了後にprose-only control auditへ戻る。
