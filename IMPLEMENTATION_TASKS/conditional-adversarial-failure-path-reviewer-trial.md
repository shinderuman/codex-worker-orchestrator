# Task: 条件付きfailure-path reviewerの限定trial

## Original instruction

```text
2026-09-28 Sol decision: System-One review feedback評価の案Bはproduction adoptionではなく限定trialとして進める。外部model呼出、metric/reduction会計、外部出力永続化、review/lifecycle/routing変更に限り、通常reviewerとは独立したfailure-path reviewerをshadow実行し、Sol/Codex削減とQuality Deltaを実測してからadoptionを判断する。GLM追加消費だけを停止理由にしない。
```

## Amendments

none

## Purpose

一般reviewer後もSolが拾う失敗経路findingを、限定された第二reviewerが先に捕捉できるか実測する。

## External feasibility

status: not-applicable

## Contract

- trialは通常reviewerと独立したrole/sessionでshadow実行し、canonical review・accept・routingの判定を変更しない。
- triggerはchanged pathと内容に基づき、外部model/process呼出、metric/reduction会計、外部出力永続化、review/lifecycle/routingに限定して機械判定する。曖昧な分類は記録し、成功に数えない。
- trigger成立taskを最大20件観測する。10件以上でadversarial-only真陽性が0なら早期停止を検討し、provider/schema failureや判定不能は欠測として保持する。
- adversarial-only findingはSolが真陽性・既存reviewer重複・新しいsemantic判断を区別する。追加GLM消費、Sol/Codex実消費、回避できたreview/fix wave、Quality Delta、false positiveを同じcohortで比較する。
- 計測は既存machine telemetryのtask単位recordに結び、未計測のSol token削減をproxyだけで実測成功と称しない。必要なusage比較ownerを明示し、測定不能ならadoption判定を保留する。
- trial結果から限定production adoptionまたはNo-Goをdecision class単位で決め、Goなら独立したadoption Taskをtracked化する。

## Must not

- trial中の第二reviewer結果でproduction acceptを自動変更しない。
- 常時全taskへ第二reviewerを追加しない。
- provider失敗、partial output、欠測を捕捉成功・Reduction成功へ算入しない。
- capture rateやQuality Deltaが不明なままproduction adoptionをGoにしない。

## Acceptance criteria

- trigger・上限・中止基準・rollbackがtrial開始前に固定される。
- 通常reviewerとの差、真陽性、false positive、追加GLM消費、Sol/Codex消費、Quality Deltaを同じcohortで比較できる。
- 観測が不足する場合は不足と再評価境界を明示し、通常のreview、validation、publicationを経てterminalとなる。

## Historical invariants

- Sol Highはsemantic tailに集中し、品質を悪化させずに移せる反復判断だけを下位へ移す。
- GLM追加消費は許容されるが、Reductionの実測はCodex/Sol消費とQuality Deltaで行う。

## Dependencies

none
