# Task: External ReviewからCodexがFindingを検証・Task化

## Original instruction

2026-10-25 ユーザー指示:「External ReviewのほうはFindingをCodex自身に見つけさせるタスクにしようと思う」「External ReviewのPRを列挙して『このExternal ReviewからFindingを見つけてタスク化する』というIMPLEMENTATION_PLANを作る」「このタスクは先頭、つまりActiveタスクとして登録する」。

## Resolved references

対象Review PRは #1343, #1345, #1349, #1353, #1354, #1364, #1368, #1375, #1381, #1390, #1391。前9件はExternal Review管理台帳 #1344 に記録、最後2件は同じHEADに対する異なるbaseの現行PRなので重複指摘を排除する。末尾の『なおExternal Reviewについては以下も追加しろ』の具体的な続きは未提示であり、推測追加しない。

## Amendments

none

## Purpose

全PRのReviewコメント、対象SHAとdiff、現在main、既存IMPLEMENTATION_TASKSをCodex自身が照合する。

## External feasibility

status: not-applicable

## Contract

全PRのReviewコメント、対象SHAとdiff、現在main、既存IMPLEMENTATION_TASKSをCodex自身が照合する。主張の再現性、実害、root/owner、過去修正の実効性を評価し、採用するものだけ既存Taskへ統合、またはsemantic slugの新TaskとしてOriginal/Contract/Acceptance付きで登録しPlan優先度を更新する。

## Must not

AI指摘を無条件採用・一括実装しない。Review PRを勝手にmergeしない。GitHub Issue番号を実装仕様の正本としない。ユーザー承認なしに次Taskを先取りしない。

## Acceptance criteria

全11 PRの独立した採否台帳（KEEP/既存Owner/修正済み/反証/証拠待ち）、実source根拠、重複処置とタスク化した正確な範囲が揃う。0件採用も正当な結果。Task/Planは独立Reviewと必要なvalidationを受け、Publication/Retireの後NEXTを開始せず停止する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
