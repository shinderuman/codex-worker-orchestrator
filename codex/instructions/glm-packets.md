# GLM結果処理

worker/reviewer packet、`glm-parent-action`のterminal envelope、またはbounded recovery projectionを受け取った場合に適用する。構造とlifecycle transportはmachineを正とし、親Codexは意味・risk・correctnessを判断する。

## 共通

- packet structural validityは`control:packet-schema-result`がfail closedで強制する。親はstructural schemaを再検証・再構成しない。
- `status:"parent_action_terminal"`では`terminal`をauthoritative semantic result、`handoff`をcanonical handoff / next-action authorityとして扱う。envelope statusをPASS/NEEDS_SOL_*等のsemantic statusに読み替えない。
- 次操作はhandoffの`consistent`・`required_action`・`allowed_actions`・`action_specs`から選ぶ。packet本文や`--status` fieldからadmissionやcommand lineを再構成しない。`consistent:false`では次actionを推測しない。
- `action_specs`のdirect actionはexact `command`、staged actionはexact `prepare_command`をtransport authorityとする。prepare後はmachine-declared `slots`だけをsemantic値へ置換し、`next_command`を実行する。path/token/placeholder/argument orderを親で組み立てない。
- terminal transport/parse failureで子stateが不明なら、最初にread-only `glm-worker --handoff recovery`だけをbounded recovery入口として使う。`last_material`、guard/quality failure projection等で足りる事実をtelemetry/session/artifact全体から再探索しない。不足が残る場合だけexact locatorに限定して追加evidenceを読む。
- 同一decision leaseのduplicate projection拒否はmachine dedupであり、同じbodyを再取得する理由にしない。複数surfaceが必要ならmachineのevidence batchを使う。
- `artifacts`は必要な成果物だけ記載pathから読む。packetへ全内容を転載しない。原因不明runtime failureは`~/.codex/instructions/failure-evidence.md`のbounded evidence条件に従う。

## `"status":"NEEDS_SOL_DECISION"`

- `decision`・`evidence`・`options`・`recommendation`・`test_obligations`を評価し、Sol Highがsemantic dispositionを決める。
- protected instruction変更を親適用する要求では、承認したtargetだけへ最小変更を行い、instruction baseline rotationが必要なら既存machine controlを使う。reject時はinstruction surfaceを変更しない。
- packetで判断できるならrepoを再探索しない。不足する場合だけ`targets`へ限定する。
- No-Goがmachine-admittedされ、semantic判断もNo-Goならhandoffのdirect action specを使う。同じNo-Goをworkerへdecisionとして再送しない。
- それ以外のdecisionはhandoffのstaged action specを使う。元依頼を再記述せず、decision内容とexecution-unit/milestone内容だけをmachine-declared slotsへ渡す。PoC/observationのGoは`~/.codex/instructions/feasibility-gate.md`に従ってtask declarationをimplementationへmigrationしてから継続する。

## `"status":"PASS"`

- 圧縮packetについて要求との意味的一致、要求漏れ、矛盾、残余riskを評価する。
- LOW riskかつ不整合・不確実性がなければ、GLMの調査をやり直さず全diffも読まない。
- PASSを機械的に信用せず、最終semantic判断はSol Highが行う。

## `"status":"NEEDS_SOL_REVIEW"`

- `targets`と`sol_question`に限定して実コードまたはdiffを確認する。reviewer summaryだけで現物確認を代用しない。
- file:line・symbol・行範囲等で絞られている場合はchanged hunkまたは狭いsource近傍から読む。不足した対象だけ段階的に拡張し、target外を予防的に先読みしない。
- 修正が必要ならCodex自身で編集せず、handoffのstaged fix action specへsemantic fix内容を渡す。origin/cause/accepted-scopeの意味判断は`~/.codex/instructions/glm-execution.md`に従う。独立reviewer再実行はwrapperに任せる。
- quality policy surface承認がmachine-admittedされている場合、承認するかsemantic fixへ戻すかだけを判断する。承認時のexact command/required parameterはdirect action specを正とし、packet自由文から組み立てない。

## finalization evidence

- semantic review後にcurrent snapshotのquality validationが必要なら既存`finalize-check` machine surfaceを使い、quality gate/result/status/handoffを別々に再取得しない。
- `ready_for_parent_decision`はvalidation/snapshot整合のmachine evidenceであり、accept/fix・task完了のsemantic判断そのものではない。
- blocked結果は同梱failure/evidenceだけを確認し、validation failure・lifecycle inconsistency・snapshot change・Git ambiguityを勝手に修復しない。
- semantic acceptance後のparent completion、remote sync、session rotationはmachine lifecycleを正とする。親は必要なGit/metadata変更の意味を判断するが、completion postconditionやremote OID一致を推測しない。

## `{"error":{"kind":"worker_error",...}}`

- error JSONとnon-zero exitを正とし、エラー要約に無い原因を広いrepo探索で補完しない。
- session破損が明示されている場合だけ既存reset/recovery boundaryへ進む。known failure modeはmachine projectionを使い、unknown failureだけをCodexのdebug判断へ戻す。
