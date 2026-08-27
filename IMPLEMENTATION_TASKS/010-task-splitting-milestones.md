# Task: task事前分割とsemantic milestoneを運用評価

## Original instruction

````text
## Task 010: task事前分割 / semantic milestoneの運用評価

Task 009のデータを使い、

-巨大taskの事前分割
-意味milestone checkpoint
- resume boundary

が品質を落とさずcall長大化を抑えるか評価する。

このTask management再設計自体の導入後データも使う。

hard capは証拠なしに導入しない。
````

## Amendments

- 2026-08-23 telemetry baseline:

````text
codex-configの保存済みtelemetryではworkerがmodel時間の約81%を占め、task累積400 turn超のoutlierが複数存在する。一方、task難易度と分割有無の対応は現在のtelemetryだけでは確定できない。

Task 009でoutlierを再現可能に特定した後、該当taskのtask責務・milestone・resume境界と照合すること。turn数だけを原因として分割せず、分割による追加review callと品質結果を併記すること。

分割によってGLM消費が減ってもCodex側touchpoint・review量が増える可能性がある。採否はCodex ReductionとQuality Deltaを上位に置き、GLM消費削減だけで採用しないこと。
````

## Purpose

worker call長大化を機械的切断ではなく責務境界で抑える。

## Contract

- task file粒度とobserved call dataを対応付ける
- quality/call cost/追加call数を併記する

## Must not

- hard cap、無条件rotation、品質証拠なしの分割強制を行わない

## Acceptance criteria

- 分割/milestone/resumeの比較と採否条件
- session rotationとは別論点で結論
- 独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- Task 009完了済みのworker outlier report、session aging観測
- F9 reviewer diff-first searchはPR #19のSquash Merge commit `948ea31ee4381f4192afeff607c015696349a9a1`でintegration済み

## Dependencies

none

## Review findings

none

## Current boundary

EVALUATED。tracked evidenceではworkerがmodel時間の約81%を占め、Task 009以前の観測に400 turn超outlier、別引継ぎ証跡に4 call/154 turnと11 call/535 turnのtaskがあるが、turn/call数と責務分割の因果・Quality Delta・Codex actual usageは確定できない。事前分割は既存Rulesどおり独立acceptance・rollback・責務境界がある場合だけ採用し、turn hard capや自動分割は採用しない。semantic milestoneは追加review/model callを発生させるproduction強制境界としては採用せず、既存task/state/commit境界を変更しない。resumeはrate-limit/provider interruption等の実停止から同一task/sessionを継続する既存境界を維持し、長さだけを理由にproactive resume/rotationしない。session rotationはTask 105の別論点として扱う。将来、同種責務の比較可能母集団でQuality Delta非悪化かつCodex Reductionを損なわず、追加review callを含むtotal cost改善が再現された場合だけ分割/milestone境界の追加強制を再評価する。
