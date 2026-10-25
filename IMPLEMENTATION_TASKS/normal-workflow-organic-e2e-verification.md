# Task: 例外なしの正常GLM Workflowを実Taskで受入検証

## Original instruction

2026-10-25 ユーザー指示:「このAuditで現在の進め方を一区切りにしようと思う」「そして最終的なFindingはCodexに実装させようと思う」。

## Resolved references

Umbrella #1348の終了条件。#1346 F01/#1356 F01/#1376 P04/#1377 R03/#1382 T01/#1383 U01〜U03/今回#1392 V01〜V05では、合成回帰テストPASSと例外的な公開成功は通常の初回Worker→Reviewer完遂とは異なる。

## Amendments

none

## Purpose

短いTASKだけの新規セッションから、no-codeとruntime変更の2classを、GLM Worker、Solの必要判断、独立Reviewer、fresh Go等、install/smoke、Bundle固定、Candidate/Publication/Retireまで追う。

## External feasibility

status: not-applicable

## Contract

短いTASKだけの新規セッションから、no-codeとruntime変更の2classを、GLM Worker、Solの必要判断、独立Reviewer、fresh Go等、install/smoke、Bundle固定、Candidate/Publication/Retireまで追う。中断/旧generation Finding/失敗Gate/親metadata reentryも含め、実sourceと実modelを使うlive evidenceを優先する。

## Must not

単なるsource testや合成Worker fixture、ユーザー承認によるCodex直接実装を通常GLMの成功証拠としない。勝手に本番Task/remoteを公開しない。

## Acceptance criteria

少なくとも一つの新規実Taskで追加の運用指示・State surgery・一時operatorなしに最初から終端まで走り、NEXTを開始せず停止する。no-codeとsource変更のE2Eを別途検証する。できなければNo-Goと未達ownerを記録する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
