# Goal起点のproject orchestration

Plan管理repositoryで`IMPLEMENTATION_PLAN.local.md`のoptional `## GOAL`節を使い、Goalだけを入口としてproject completionまで開発を継続する場合の親Codex手順。GLM worker/reviewer実行、per-task lifecycle、parent action、rate-limit / Codex-limit recoveryは既存規則をそのまま使う。GOAL節がないrepositoryでは本手順を使わず、従来のPlan運用のみを行う。

## Goal受領とtracked化

- GoalをUSER_REQUESTへ置いたまま作業を続けない。最初のGLM委譲前にGOAL節へ固定する。GOAL節はGoal原文のlosslessな保存、append-onlyのgoal amendments、derivedなacceptance criteria、そして`status: active`行を持つ
- GOAL節とPlanは親Codex専有metadataであり、GLM worker/reviewerは編集しない
- Goalのtask分解は通常の要求判断として行い、初期task file群を`IMPLEMENTATION_TASKS/`へ作成してPlanのACTIVE / NEXTへ置く。分割・tracked化の境界は`IMPLEMENTATION_RULES.md`の要求受領規則に従う
- 先行成果物が必要なtask間依存は、各task fileの`Dependencies`へ`IMPLEMENTATION_TASKS/`相対pathで明示する

## 進行とtask選択

- 各局所終端(packet受理・commit・install後)に、sandbox内でread-onlyの`glm-worker --project-state`を1回実行し、schedule、dependency graph、next_runnable、blocker、completion readinessを機械参照する
- 投影は編集を行わない。次taskのACTIVE昇格、priority変更、cancel、追加、replanningは親CodexがPlanとtask fileへ直接反映し、再度投影で整合を確認する
- 投影がunknown参照、self dependency、cycle、malformed GOAL / schedule / task contractでfail closedした場合、GLM側の再実行では解決しない。親CodexがPlan / task fileの該当記述を修復してから同じ投影を再実行する
- 実行開始は`glm-parent-action start`、decision・fix・accept・resumeも既存の親actionをそのまま使う

## user amendment

- Goal自体への追加要求・制約変更は、原文をGOAL節のgoal amendmentsへ省略せず追記し、矛盾する旧要求も削除せず最新をderived acceptanceへ反映する
- 個別taskへの追加要求は`~/.codex/instructions/task-request-boundary.md`の境界でtask fileの`Amendments`へ反映する。amendmentを理由にACTIVE taskを無条件に破棄しない
- amendment後の継続・再計画・安全停止の判断は親Codexが影響範囲と依存関係から行う

## 停止・再開

- rate limit、provider停止、Codex 5h limit、session終了、compaction後は既存の`glm-auto-resume.md`、`codex-auto-resume.md`、`IMPLEMENTATION_RULES.md`の再読contractで同じprojectを継続する
- wake後・再開時はRules、Plan、GOAL節、ACTIVE task fileを現在checkoutから再読し、conversation memoryを正としない。projectの進行状態は`--project-state`投影で再構成できる

## completion

- 最終taskの局所終端で、`--project-state`のcompletion readinessがreadyであることと、親CodexによるsemanticなGoal acceptanceを両方確認する。readinessは機械条件の投影であり、単一worker PASSをproject completionへ昇格せず、必要なinstall・Git publication判断を代替しない
- 親acceptance後、GOAL節へboundedなcompletion decisionを記録して`status: completed`へ変更し、ACTIVE / NEXT / BLOCKEDを空へ同期した同一commitで確定し、既存commit / install規則へ合流する
- この順序の外で未完了GoalのACTIVEを空にしない。completed GOALと空scheduleはterminal状態のみが許可する
