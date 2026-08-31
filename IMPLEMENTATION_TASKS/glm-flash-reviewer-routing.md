# Task: GLM-4.7の実運用routingとGLM-5.3-Flashへの置換を評価する

## Original instruction

````text
要求:

- 現在GLM-4.7を利用している実運用routingを確認する。
- GLM-5.3-Flashが同じ役割をより適切に担えるなら置換する。
- 特に初回低リスクreviewerの置換を評価する。

制約:

- GLM-5.3 worker、高リスクreviewer、fix後reviewのroutingは必要性なく変更しない。
- model名の一括置換はせず、GLM-4.7の各利用箇所の役割を確認する。
- Codex ReductionとQuality Deltaを優先し、GLM token削減自体を主要目的にしない。
- 追加の評価専用model callや複雑なrouting frameworkを作らない。
- 後方互換性は維持しない。

完了条件:

- GLM-4.7のcurrent production利用箇所を特定する。
- 5.3-Flashへ移す箇所と残す箇所を根拠付きで確定する。
- 採用する場合はrouting/config/testをcurrent canonical modelへ更新する。
- validation後、通常workflowで完了する。
````

## Amendments

none

## Resolved references

- 「021」は`IMPLEMENTATION_TASKS/021-conditional-improvements.md`を指す。実行順序はPlanを正とし、番号や配置順をhard dependencyとは扱わない。
- 今回の依頼はタスク登録であり、調査・評価・実装の開始指示ではない。
- 「通常workflow」は`IMPLEMENTATION_RULES.md`の品質確認、task完了、commit / installの規則を参照する。

## Purpose

Codex ReductionとQuality Deltaを優先し、初回低リスクreviewerを中心に、GLM-4.7の各実運用上の役割にGLM-5.3-Flashが適するか判断する。

## External feasibility

status: observation
assumption: GLM-5.3-Flashがcurrent production providerと実行環境で利用可能であり、追加の評価専用model callを行わずに既存の実運用証拠から対象routingへの適合性を判断できること。

## Contract

- current productionのGLM-4.7利用箇所を役割単位で特定し、設定・dispatch・検証の対応を確認する。過去資料や非production用途との区別を明示する。
- 役割ごとに移行または維持の根拠と品質・Codex消費への影響を整理し、特に初回低リスクreviewerの採否を親Codexが確定する。GLM token削減だけを採用根拠にしない。
- GLM-5.3-Flashの利用成立性と根拠の不足を明示し、未検証の能力・効果を断定しない。追加の評価専用model callは行わない。
- 採用箇所だけrouting/config/testをcurrent canonical modelへ揃える。旧routing・旧model名の後方互換性は維持しない。
- 必要なvalidationと独立review、親Codexの最終採否を経て、通常workflowで完了する。

## Must not

- GLM-5.3 worker、高リスクreviewer、fix後reviewのroutingを必要性なく変更しない。
- 役割を確認せずmodel名を一括置換しない。
- GLM token削減自体を主要目的にしない。
- 追加の評価専用model callや複雑なrouting frameworkを作らない。
- 後方互換のfallback、alias、dual routingを追加・維持しない。
- 未確定のrouting採否をGLMだけで最終決定しない。

## Acceptance criteria

- GLM-4.7のcurrent production利用箇所と各役割を特定している。
- 初回低リスクreviewerを含め、5.3-Flashへ移す箇所と残す箇所を根拠付きで確定している。非採用の場合も理由を残す。
- 採用する場合はrouting/config/testがcurrent canonical modelへ揃い、変更箇所と保護対象routingのvalidationが成立している。
- Codex ReductionとQuality Deltaを優先した判断であり、追加の評価専用model call・複雑なrouting framework・後方互換維持を導入していない。
- validation後、`IMPLEMENTATION_RULES.md`の通常workflowで完了している。

## Historical invariants

- 親Codexの意味判断・最終採否、独立review、validation authority、parent-managed metadata guard、GLMのcommit/push禁止を維持する。
- 最上位評価はDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaとする。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。2026-09-01のユーザー依頼により登録した。production利用箇所の調査、GLM-5.3-Flashの成立性確認、採否判断、routing/config/testの変更は行っていない。
