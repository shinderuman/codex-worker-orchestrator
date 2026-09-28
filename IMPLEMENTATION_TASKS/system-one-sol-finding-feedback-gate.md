# Task: Sol findingのreview feedback経路

## Original instruction

```text
2026-09-28 Sol decision: System-One review feedback評価で確認したF1〜F5を起点に、Solが一度確定した反復可能なfindingをtask contract、対象surfaceのpinning test、reviewer charter、deterministic gate候補へ戻す独立経路を作る。未確定の品質policyはSol判断として残し、実装済みの条件だけを機械またはGLMへ移す。
```

## Amendments

none

## Purpose

Solの発見が次回も同じSol再検出を要しないよう、明示済み要求とtest/review義務を継続して反映する。

## External feasibility

status: not-applicable

## Contract

- F1〜F5を明示要求miss、導出可能な失敗経路、未確定semantic policyに分け、各classの正規ownerを定める。
- 確定済みの失敗経路条件を対象surfaceのpinning testまたは狭いdeterministic gateへ移し、reviewer charterは残余のfailure-path観点へ限定する。
- F5型のrequired field presence/nullとF2/F4型の失敗時会計・永続化を、再現可能なfixtureで扱う。
- 変更前後の捕捉範囲、追加GLM消費、Sol finding再発を比較できるevidenceを残す。

## Must not

- 未確定の閾値・品質policyをGLMやgateが自動確定しない。
- 全taskへ常時第二reviewerを追加しない。
- 既存reviewer charterを無制限に長文化しない。
- 現在のSystem-One shadow evaluatorをproduction routingへ昇格しない。

## Acceptance criteria

- finding classごとのownerと決定可能なgate/test境界が固定される。
- F2/F4/F5の代表的なfalse successを対象testが防ぎ、残余semantic tailが明示される。
- Codex/Sol削減とQuality Deltaの測定入口を備え、通常のreview、validation、publicationを経てterminalとなる。

## Historical invariants

- Codex ReductionとQuality Deltaを最上位Evalとする。
- Sol Highは曖昧・高risk・高レバレッジな判断へ集中させる。

## Dependencies

none
