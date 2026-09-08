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

### 2026-09-08（authority訂正）

````text
改めていうがCodexのPushは許可している
GLMのPushは許可してない
Codexは監督者、GLMは作業者だからだ
監督者を飛び越えて作業者がリモートにPush作業するのはおかしいだろ
レビューをせずに作業を「完了」させているということなんだから
なんて勘違いしてるんだよ
ちゃんと直せ
````

## Resolved references

- 2026-09-08、local `main` のcommit `f9c4c14` と`1cc47a1`を`origin/main`へ通常pushする親操作が外部安全審査で拒否された
- 拒否理由は、対象branch・remoteへのユーザー明示承認が確認できない状態でdefault branchへ2 commitを外部writeするriskだった
- 初回実装で`glm-parent-action push-binding`はexact remote/ref・authorization state・remote postconditionを返すようになった
- 初回完了直後、local `main`が`origin/main`より3 commit aheadのままtask metadata同期とsession rotationへ進んだ。新sessionで手動実行した`push-binding`は`authorization:user_decision_required`を返しており、判定commandの存在だけでは通常completion lifecycleを拘束できていない
- 2026-09-08、親Codexがcurrent HEAD `4d8bab88b4ad59f3483cd98422ad09fe4cac553e`を`origin/refs/heads/main`へ通常pushし、installed `push-binding --expected-oid ... --attempt-outcome completed`が`classification:synced`、`postcondition.met:true`を返した。即時のremote差異は解消したが、親が手動で気付くことへ依存するlifecycle gapは未解消である
- 2026-09-08、ユーザーは監督者であるCodexの通常pushを恒久許可済みであり、remote write禁止は作業者/reviewerであるGLMだけに適用すると再確認した。Codexまで`user_decision_required`へ戻した初回解釈はauthority境界の誤りである

## Purpose

親completion flowがGit remote writeの権限・実行・拒否・再試行境界を曖昧にせず、local完了をremote同期済みと誤認したり、後続task・session rotationへ未通知のahead commitを累積したりしないようにする。

## External feasibility

status: implementation

assumption: 親Codexの恒久push authorityとGLMのremote write禁止を分離し、review後の親completion pushとremote postconditionを機械的に拘束できる境界が存在する
evidence-source: producer
evidence: 2026-09-08の実completionで、初回`push-binding`実装後も手動commandを呼ばないままaccept・task同期・rotationへ進みlocal mainが3 commit aheadになった。その後、親Codexによる通常pushは成功し、live remote postconditionもmet:trueになった。ユーザーはCodexのpushは恒久許可済み、GLMのpushは禁止と明示した
go: 2026-09-08 Sol High判断。review・Sol採否・validation後に親Codexがcommit/install/metadata同期と通常pushを行い、live remote postcondition成功後だけtask完了・rotation・次task開始を許可する。GLM worker/reviewerにはremote write authorityもpush実行surfaceも与えない

## Contract

- 親Codexはreview・Sol採否・必要validationを終え、implementation commit・install/smoke・task metadata同期commitを確定した後、configured upstreamのexact remote/refへ通常pushする。既存の恒久authorityに対する都度のユーザー承認を要求しない
- GLM worker/reviewerはremote writeを実行せず、親Codexのreview・採否・commit・push責務を代行しない
- remote-sync-pendingが未解消の間は、親USER_REQUESTのtask完了、次taskのmodel開始、session rotation完了のいずれも成功扱いにしない。review結果の受理・親accept・local commit・install・metadata同期はpush前の必要工程として妨げない
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
- 親Codexの恒久push authorityを`user_decision_required`へ縮退し、同じ許可をtask/remote/refごとに取り直さない
- GLMから到達可能なremote write commandやcredentialを追加しない

## Acceptance criteria

- 親Codexの恒久authority下で、review/validation前のpushは拒否され、review/validation・commit/install/metadata同期後のconfigured upstreamへの通常pushは追加承認なしで正規工程として実行される
- remote-sync-pendingのまま親USER_REQUEST完了・次task model開始・session rotation完了を試みる代表scenarioがfail closedし、親accept・local metadata同期はpush前工程として成立する
- GLM worker/reviewerからremote writeを試みる代表scenarioがmodel実行前またはcommand admissionで拒否される
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

false-completeとしてACTIVE再開中。Codex/GLM authorityを上記の監督者/作業者モデルへ訂正し、review後の親pushとremote postconditionを親USER_REQUEST完了・次task model開始・rotation完了へ機械bindingする。
