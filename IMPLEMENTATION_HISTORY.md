# codex-worker-orchestrator 例外decision記録

このfileは通常taskの完了ledgerではない。ordinary completionのcommit・diff・validation・install・runtime evidenceはGit、CI、bundle / telemetryを正とし、task要求は削除済みtask fileのGit履歴から回収する。通常task完了時にこのfileを読んだり追記したりしない。

親Codexだけが編集し、GLM worker/reviewerは読み取り専用とする。新しいrecordは次をすべて満たす場合だけ追加する。

- production diffやcurrent Rules/task contractだけでは表現されないcross-taskの採否・Go/No-Go decisionである
- 将来のtracked taskがそのdecisionをactivation / adoption条件として明示参照する
- raw transcript、検証chronology、commit一覧を複製せず、decision・根拠の最小要約・再評価境界だけで足りる

参照taskがなくなったrecord、またはcurrent Rules/task contractへ移行して過去decisionを読む必要がなくなったrecordは削除する。escaped defectの詳細診断を保存するためにこのfileを増やさず、再発防止として残す必要がある意味契約はcurrent Rules/task/testへ移す。

## 2026-08-28 Task 012 compaction threshold evaluation

- decision: No-Go。保存済み20 task・69 model call中、compact boundaryは4 call / 4件だった。
- limitation: trigger直前context sizeとcompaction要約costを当時のevidenceから確定できず、transport上のsemantic marker保持だけでもpost-compactionの意味適用を証明できない。
- reevaluation boundary: `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`のpermission / activation contractに従い、同形式の実測とsemantic quality evidenceを揃えてからthreshold変更を再判断する。

## 2026-08-29 Task 013 worker model routing evaluation

- decision: No-Go。current codex-config telemetryはsingle resolved model `glm-5.3`だけで、alias差からmodel品質差を評価できない。
- reevaluation boundary: `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`に従い、同一repository・role・normalized phase・effective risk・convergence deltaで複数resolved modelを比較できる実運用evidenceとユーザー許可が揃った時だけrouting変更を再判断する。

## 2026-08-23 Claude CLI compatibility preflight

- decision: runtime fail-closed preflightは不採用。test-only checker、依存flag inventory、help snapshot、live no-AI canaryだけを最小採用した。
- tradeoff: PoC時点のruntime overheadは約0.25秒、見込んだ親Codex診断削減は1〜2 turnで、help format依存によるfalse rejectと全task停止riskを上回る採用根拠がなかった。
- reevaluation boundary: `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`に定義した実互換障害、診断turn反復、またはflag/help drift canaryの実失敗が観測された時だけ再評価する。
