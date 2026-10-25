# Task: 不正Worker/Reviewer Packetの有限補正と正規再入

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

Worker NEEDS_SOL_DECISIONのschema不一致、Reviewer TARGETS/proof/byte budgetと再提出の実停止（#1347 J04/J05、#1356 F03/F04、#1359 G02/G03、#1377 R04、#1382 T10）。今回#1392でもReviewer invalid_packetが2件。

## Amendments

none

## Purpose

既存Packet schema・モデル出力契約・Task/Attempt/Review sessionを照合し、修正可能な違反のみ限定feedbackで再送させ、必要なSol判断・独立Reviewへ正常復帰する。

## External feasibility

status: not-applicable

## Contract

既存Packet schema・モデル出力契約・Task/Attempt/Review sessionを照合し、修正可能な違反のみ限定feedbackで再送させ、必要なSol判断・独立Reviewへ正常復帰する。既存 resubmit-report / correction ownerの実効性を先に検証する。

## Must not

不正packetをPASSへ格上げしない。Schema互換や自由形式JSONでGuardを迂回しない。独立Reviewのsemantic指摘を削らない。

## Acceptance criteria

非法TARGETS/size/decision packet→有限corrected packet→独立Review/最終Gateへの正常復帰を現行production入口で再現する。旧source Reviewの再利用は拒否し、無意味な追加モデルcallを測定・低減する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
