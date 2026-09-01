# Task: conditional review/tool output改善

## Original instruction

````text
## Task 021: conditional review/tool output改善

Task 008/009/011等の結果で効果が確認されたものだけ採用。

事前に「便利そう」で追加しない。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
## 4. Task 021は通常のGLM implementation taskではなくparent decision gateとして扱う

`021-conditional-improvements.md`は自身ですでに、

* umbrellaをGLMへ直接dispatchしない
* measurement結果でcandidateを採否
* 採用candidateは別task fileを作る

としています。

これは通常implementation taskではありません。

親Codexのevaluation / decision checkpointです。

### 修正

Task 021をGLM workerへそのままdispatchしないことを維持してください。

実行時は親CodexがTask 008 / 009 / 011等のartifactを読み、

* 採用
* 棄却
* data不足で保留

を判定するだけにしてください。

採用する実装は必ずsemantic filenameの独立taskを作成する。

棄却結果はHistoryへ記録する。

Task 021自身のためだけに通常implementation task同等の、

* GLM implementation
* 独立reviewer
* Sol gate

を機械的に回さないでください。

必要なanalysisが大きい場合だけworkerへread-only調査を委譲してください。
````

- 2026-09-01 parent maintenance:

````text
IMPLEMENTATION_HISTORY.mdは通常taskの完了ledgerではなく、将来のtracked taskが明示参照する非diffのcross-task decisionだけを保持するbounded exceptional recordへ変更する。
Task 021の棄却結果も無条件にはHistoryへ追加せず、将来の採否を実際に拘束し、current Rules/task/Git/bundleだけでは十分に表現できないdecisionだけをcompactに残す。
````

## Purpose

親Codexのdecision checkpointとして、品質証拠なしの複雑性増殖を防ぐ。

## External feasibility

status: not-applicable

## Contract

- candidateごとにevidence、expected reduction、quality risk、rollbackを示す
- 親CodexがTask 008 / 009 / 011等のartifactから採用・棄却・data不足を判定する
- 採用変更はsemantic filenameの個別taskへ分割する
- 棄却は通常completion ledgerへ記録しない。将来のtracked taskが採否条件として明示参照し、current Rules/task/Git/bundleだけではdecisionを十分に表現できない場合だけ、`IMPLEMENTATION_HISTORY.md`へdecision・最小根拠・再評価境界をcompactに残す

## Must not

- このumbrellaをそのままGLM実装taskへ渡さない
- 通常implementation同等のGLM implementation、独立reviewer、Sol gateを機械的に回さない。大規模analysisだけread-only workerへ委譲できる
- 採用/棄却chronologyをHistoryへ無条件appendしない

## Acceptance criteria

- candidateの採用/棄却/保留を測定結果で決定
- 採用分は新task fileへ分離
- 棄却decisionをHistoryへ残す場合は、将来参照するtracked task、非diff decisionである理由、再評価境界が明確である

## Historical invariants

- complexity responsibility、conditional convergence方針
- Task 008測定ではmachine JSONのbytes/token proxy削減は不立証、意味保持は31/31（旧形式27/31）であり、継続採用根拠は単一contractとQuality Delta
- Task 009 worker outlier report完了済み
- Task 011でoperation categoryの10値閉集合と保存済みeventからのcategory別集計経路が成立済み。timeline JSON presentation拡張は必要性不立証として不採用。

## Dependencies

none

## Review findings

none

## Current boundary

Task 011完了済み。Task 008/009/011等の測定artifactを親Codexが採否するdecision gate待ち。残る測定task完了前は直接dispatch禁止。
