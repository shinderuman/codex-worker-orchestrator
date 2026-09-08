# 親Codex session rotation

同一saved projectの親Codex sessionを、task境界で機械的に新規作成し直す運用。親contextの累積再入力によるCodex消費増大を、task境界で抑える。compaction対応やGLM側rate limit再開とは別論点で、それらの規則を変更しない。

## 前提

- rotationは恒久許可済みで、rotationごとのuser再承認を求めない。
- 評価・directive保存はglm-workerが`--accept` / `no-go`のmaterial terminalと同じ境界で行う。`glm-worker --handoff`（通常・recovery・evidence handoff part）は保存済みstateのread-only投影で、marker更新やclaimを行わない。
- rotation専用のdaemon・DB・watcher・照会commandは使わない。判定根拠はhandoffの`session_rotation` fieldだけを読む。
- healthyなGLM in-flight呼び出しをrotationのために中断・再起動しない。実行中taskの完了境界を待つ。

## handoffの`session_rotation` field

- `state=pending`：`directive`（directive_id・task_id・terminal・reason・evidence）が1件だけ入る。この親sessionは既にrotation判定を満たしている。
- `state=not-required`：直近のterminal評価でrotation不要だったもの。`last_evaluation`に評価時点のtask・terminal・required・reasonが入る。
- `state=unavailable`：parent thread identity未bindまたは評価記録なし。`reason`に理由が入る。この状態で新規task作成を急ぐ必要はない。
- `reason=evidence-unavailable`は、rollout成果・live 5h limit読取などの証拠が取れなかったため保守的にrotation requiredとしたことを示す。Codex削減や品質の改善証拠としては数えない。

## pending directiveを見たときの操作

1. thread作成前に旧threadで`glm-parent-action rotation-claim <directive-id>`を実行し、`claim_id`を保存する。同じcommandの再実行は同じclaimを返す。
2. 新規taskは同じsaved projectに所属させ、同じlocal checkoutで作業させる。別worktree・別cloneを作らない。現在checkoutに紐付くGLM runtime stateを失わせない。
3. thread作成成功後、旧threadで`glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>`を実行する。作成失敗が確定した場合だけ`glm-parent-action rotation-fail <directive-id> <claim-id>`でclaimを解放して再試行する。作成結果が不明なら解放・再作成せず、作成済みthreadを確認する。
4. 新規taskの最初の依頼はbootstrap要旨だけにする。`IMPLEMENTATION_PLAN.local.md`のACTIVE task再読、Git現物確認、保存済みGLM task/session stateの再取得を指示する。旧会話の自由文・要約・ACTIVE task本文を要求正本として複製しない。
5. 新threadは`glm-parent-action start --rotation-claim <claim-id>`を実行する。milestone開始では`glm-parent-action start-milestones <token> --rotation-claim <claim-id>`を使う。開始処理はbind対象thread・claim・予定task IDを照合し、途中失敗後も同じclaimで同じtaskへ再開する。milestone tokenを消費済みなら`prepare start-milestones`で新しいtokenを作る。worker再開checkpoint保存後にclaimをacknowledgeしてdirectiveをretireする。
6. 旧sessionの親taskは、新規taskの最初のparent action確認後に終了させる。GLM in-flight呼び出しが残っている場合はその完了を待つ。

## trigger（参照）

glm-workerは同一parent threadで次のいずれかを満たした時にpending directiveを保存する。

- semantic acceptanceまで完了したtaskが2件（default）
- 今回acceptしたtaskのterminal riskがHIGH（1件でrotation）
- 親rolloutのcompaction発生
- 親rolloutのmodel return 6回以上
- 親rolloutのmodel-visible tool出力が256 KiB以上
- 同一task内のmaterial event（review fix・guard/conflict recovery・Sol decision）が2件以上
- 親5h windowのused_percentがsession開始baselineから10 percentage points以上増加

親側でこの閾値を再計算・再判断しない。handoffの`session_rotation`だけを正とする。
