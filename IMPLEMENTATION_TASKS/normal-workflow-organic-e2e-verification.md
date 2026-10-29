# Task: 例外なしの正常GLM Workflowを実Taskで受入検証

## Original instruction

2026-10-25 ユーザー指示:「このAuditで現在の進め方を一区切りにしようと思う」「そして最終的なFindingはCodexに実装させようと思う」。

## Resolved references

Umbrella #1348の終了条件。#1346 F01/#1356 F01/#1376 P04/#1377 R03/#1382 T01/#1383 U01〜U03/今回#1392 V01〜V05では、合成回帰テストPASSと例外的な公開成功は通常の初回Worker→Reviewer完遂とは異なる。

### 2026-10-29 Dogfood #1432 — レビュー専用委譲違反を正常E2Eと区別

- Bundle `0b1b3ae0-f403-41f7-9f60-d9f5e847d07b.zip` の親生transcriptに、08:47:50ZユーザーがGLMを**独立reviewer限定**とした後、親が09:14:48ZにGLM `worker-decision`を誤起動し、09:27:01Zユーザー訂正→09:27:12Z STOP、出力した`cmd/precallrepro`を隔離した経過が残る。
- 最終Taskの修正・調査はユーザー特例のCodex直接実装。後続のGLM Reviewer 2回を含めても、正常Worker→Reviewerの無人E2E成功証拠ではない。**実装者ロールと親の明示された委譲範囲をsource-boundに対応付け、人手訂正・例外的State調整・拒否したWorker成果を成功件数から除外する。**
- 現時点で機械的に防止できる独立Repository role-admission欠陥が実証されたわけではない。本Taskの既存No-Go/正常E2E識別の証拠として扱い、仕様未確定の第二role policy frameworkを追加しない。

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
