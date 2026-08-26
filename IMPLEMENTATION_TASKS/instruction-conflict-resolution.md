# Task: instruction conflictをpriority/behavior contractで解消する

## Original instruction

````text
F8: instruction間にGLMが選択できる曖昧な衝突がある

代表例:
- `common-code.md`: 原因調査情報を「保持・ログ出力」
- machine contract: machine stdout/stderrへ人間向けtextを混在させない
- `cli.md`: option追加時は可能ならshort optionも追加
- global rule: ユーザー要求外機能を追加しない

どちらを優先するかモデル判断に残すと、局所的に都合のよい方を選べる。

REQUIRED HARDENING

- GLMが読むinstruction間のpriority/conflictを監査する。
- machine outputについて「ログ出力」が外部machine stdout/stderrを意味しないよう責務境界を明確化する。
- CLI short option規則が要求外public API追加を強制しないよう修正する。
- generic ruleとtask-specific/contract-specific ruleが衝突する場合のpriorityをモデルの都合解釈へ残さない。
- 同じruleを複数fileへコピーして解決しない。
````

## Amendments

- 2026-08-26 Product boundary: 本repo文章だけでなく、通常glm-worker実行時に渡されるcontract同士の曖昧な選択余地をTrack Aで減らす。
- 2026-08-26 Clarification: Track A/Bを区別し、同じmechanismで双方を満たせる場合だけ重複実装を避ける。

## Resolved references

- machine output taskは現在ACTIVEの共通serializer/ownership boundary実装を指し、F8で重複実装しない。

## External feasibility

status: not-applicable

## Purpose

generic/task-specific/machine contractの衝突をGLMが都合よく選択できるfailure classを、priorityとbehavior boundaryへ収束する。

## Contract

- 通常glm-workerが読むinstruction conflictをTrack A/Bで棚卸しし、代表caseの優先関係をbehavior testへ落とす。
- machine output/log責務とshort option/要求外APIの衝突を解消し、rule複製を避ける。

## Must not

- prose優先順位の追記だけ、rule複製、machine output taskとの二重実装で完了しない。

## Acceptance criteria

- 代表conflictがGLMの自由選択なしに同じbehaviorへ収束するregression testを持つ。
- F8のA/B分類と残存機械化不能境界を記録する。

## Historical invariants

- machine output共通boundaryとユーザーscope制限を維持する。

## Dependencies

none

## Review findings

none

## Current boundary

NEXT。EVAL責務整理後の現行surfaceを正としてF8をTrack A/Bへ分類して着手する。
