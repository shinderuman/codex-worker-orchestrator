# Task: Mechanized control prose thinning

## Original instruction

````text
あと機械化したものは自由言語での制限は薄くしていいんじゃないのか
すべて消すと機械で制限されている根拠を見失う可能性があるので全部消すわけにはいかない
だが記載を薄くすることでお前は他の判断を重要しすることになるかもしれない
なぜなら文字数が多ければ多いほどお前はルールを見落とすからだ

あとこの「課題を見つけたら課題化する」というのも機械化可能なら機械化すべきじゃないのか
````

## Amendments

none

## Resolved references

- 対象は機械化済みまたは本task以前のenforcement taskで機械化されるcontrolのRules、AGENTS、on-demand instruction、worker/reviewer prompt内の重複手順である
- 自由言語を全削除せず、なぜ制限があるか、どのmachine ownerが強制するか、何が親Solの残余判断かを見失わない最小索引を残す
- `continuous-improvement-task-capture.md`は候補捕捉・未処理state・Task/Plan反映を機械化し、本taskはその後の説明量を含む全controlのcontext負荷を削減する

## Purpose

機械で既に強制される手続きを長い自由言語で親Codexへ再実行させず、制限の根拠と残余判断を追跡可能なままfixed contextを削減し、重要なsemantic ruleの見落としを減らす。

## External feasibility

status: not-applicable

## Contract

- audit inventoryの`machine-enforced` controlごとにstable control ID、目的、machine owner、code/test/postcondition locator、残余の親判断をmachine-readable registryへ保持する
- Rules/AGENTS/instruction/promptでは、machine ownerが決定するargv、順序、retry、field検査等の手順複製を削り、control IDと目的・呼出入口・残余判断だけのcompact projectionへ置換する
- `partial`、`prose-only`、`external-unenforceable`は未強制部分を削らず、先に対応taskまたは外部境界を解決する
- registryと実装/test locatorのdrift、削除、owner不在をlintで検出し、古い説明が残り続けることも検出する
- install後に実際に注入されるglobal/on-demand instructionのbyte/token proxyをbefore/after比較する
- continuous improvement機能のmachine state/admissionが成立した後、その自由言語を同じ基準で薄くする

## Must not

- 機械強制の根拠、制限の目的、残余riskを全削除しない
- instruction文字列presence testをcontrol成立の根拠にしない
- prose削減のためにSol semantic gate、ユーザー要求、quality criteriaを削らない
- code locatorだけの壊れやすい行番号一覧を第二正本にしない
- token削減だけを理由に`partial` controlを機械化済みと偽らない

## Acceptance criteria

- audit対象の全`machine-enforced` controlにregistry entryと有効なowner/test/postcondition locatorがある
- 代表controlでinstructionから手順を削除しても、手順欠落・誤順序・禁止操作がproduction harnessで拒否される
- `partial`/`prose-only` controlの未強制部分が誤って薄くならないtestがある
- installed Codex instructionのbyte/token proxyが削減され、残るsemantic ruleとcontrol provenanceを機械確認できる
- registry/implementation/instruction projectionのdriftがharnesslintでfail closedする
- 独立reviewer、Sol semantic review、current snapshot validation、install/smokeを完了する

## Historical invariants

- 自由言語はsemantic intentとprovenanceを伝えるが、deterministic safety propertyの実行主体にはしない
- 最上位目的はCodex ReductionとQuality Deltaであり、prose削減による重要判断の欠落を許容しない

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`
- `IMPLEMENTATION_TASKS/continuous-improvement-task-capture.md`
- `IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`
- `IMPLEMENTATION_TASKS/codex-instruction-conflict-reduction.md`
- `IMPLEMENTATION_TASKS/parent-plan-continuation-enforcement.md`
- `IMPLEMENTATION_TASKS/runtime-install-completion-binding.md`
- `IMPLEMENTATION_TASKS/auto-resume-heartbeat-transaction.md`

## Review findings

none

## Current boundary

auditと各enforcement taskの完了後、実際にmachine-enforcedとなったcontrolだけを対象にinstructionを薄くする。
