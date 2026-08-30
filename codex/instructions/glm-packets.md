# GLM結果処理

`glm-worker`からpacket(stdoutのmachine JSON 1行)またはprocess失敗(stderrのerror JSON 1行とnon-zero exit)を受け取った場合に適用する。

## 共通

- 受理結果のmachine protocolはcompact 1行JSONで、keyは`status`・`risk`・status別契約field(`summary`・`decision`等のschemaと同じ語彙)・`targets`・`artifacts`・`sol_question`。空field・空配列のkeyは省かれる(`artifacts`key欠落=none、`targets`の`["none"]`=対象なしsentinel)。契約外のfieldは機械出力へ混入しない。
- packet受理後などmaterial state transitionから次のlifecycle操作を選ぶ場合は、まず`glm-worker --handoff`を実行し、`consistent`・`required_action`・`allowed_actions`を合法な親操作の正規入口とする。`consistent:false`では次操作を推測しない。packet本文や`--status`の個別fieldからaction admissionを再構成せず、`--status`は追加の詳細診断が必要な場合だけ使う。
- `artifacts`のkeyがあるなら、要求・判断・報告に必要な成果物だけを記載パスから確認し、packetへ全内容を転載しない。
- 原因不明runtime failureの診断に必要なevidenceを求めた依頼では、`artifacts`参照先を`~/.codex/instructions/failure-evidence.md`の受理条件で必要範囲だけ確認する。

## `"status":"NEEDS_SOL_DECISION"`

- `decision`・`evidence`・`options`・`recommendation`・`test_obligations`を評価する。
- `targets`がすべてrepository内の`AGENTS.md`/`AGENTS.local.md`相対pathで、packetがそのprotected instruction変更を親適用として要求している場合は、workerへ直接編集させない。rejectならinstruction surfaceを変更せず通常どおりdecisionを返す。applyならmodel processが停止している間に親Codexが`targets`だけへ承認した最小変更を適用し、`glm-worker --rotate-instruction-baseline`を実行してactive taskのinstruction baselineを明示rotationした後にdecisionを返す。rotationはtask/worktreeを保持しworker/reviewer sessionを無効化する。guard緩和やresetで代用しない。
- packetで足りるならリポジトリを再探索しない。判断不能な場合だけ`targets`に限定して現物を確認する。
- 判断後は元依頼を再記述せず、判断本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--decision-stdin <payload-bytes>`）で同じtaskへ継続する。

## `"status":"PASS"`

- 圧縮packetについて、要求との意味的一致・要求漏れ・矛盾・残余リスクを評価する。
- `"risk":"LOW"`かつ不整合・不確実性がなければ、GLMの調査をやり直さず全diffも読まない。
- PASSを機械的に信用せず、圧縮された意味情報への最終判断はSol Highが行う。

## `"status":"NEEDS_SOL_REVIEW"`

- `targets`と`sol_question`に限定して実コードまたはdiffを確認する。
- 修正が必要ならCodex自身で編集せず、修正方針本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--fix-stdin <payload-bytes>`）で同じworker sessionへ差し戻す。修正後は独立reviewerまで自動再実行される。Sol自身が現diffの残存部分を受理し、fixが撤回・縮小だけなら`--accepted-scope current-diff`を付ける。不確実または新規変更を許すfixでは付けない。

## finalization evidence

- terminal packetのsemantic reviewが終わり、現snapshotへquality validationが必要な段階では、sandbox外で`glm-parent-action finalize-check <go-test|go-test-race>`を1回使う。既存quality gate、canonical handoff、current validation/snapshot照合、read-only local Git summaryを1 machine resultへまとめるため、同じ目的の`--quality-gate`→result/status→`--handoff`→`--status`往復を別々に行わない。
- `status:"ready_for_parent_decision"`はvalidationとsnapshot整合を示すだけで、accept/fix・task完了・commit message・commit/push判断は親Codexが行う。`git.remote_state:"not_checked"`はremote freshnessを推測していないことを意味する。
- `status:"blocked"`では`failure.stage`・`reason`と同梱済みevidenceだけを確認し、validation failure・lifecycle inconsistency・snapshot change・Git ambiguityを自動修復しない。
- `~/.codex/instructions/git.md`が許可する通常fast-forward pushが成功した場合、そのpush自体をpublication結果として扱い、同じ成功を証明する目的だけの追加fetch・remote HEAD照合・post-push statusを行わない。push拒否・通信断等で成否が曖昧な場合だけ親判断へ戻す。install前のfinal HEAD・clean working tree等、git.mdが別目的で要求するlocal postconditionは省略しない。

## `{"error":{"kind":"worker_error",...}}`

- stderrのerror JSON(`kind`・`message`・`detail{phase,exit_code,output_tail}`)とnon-zero exitで示される。エラー要約を確認し、無関係なリポジトリ調査をSol Highが代行しない。
- session破損が明示されている場合だけ`glm-worker --reset`後に再実行する。
