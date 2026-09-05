# Task: Prose-only control enforcement audit

## Original instruction

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Amendments

### 2026-09-04

````text
あと機械化したものは自由言語での制限は薄くしていいんじゃないのか
すべて消すと機械で制限されている根拠を見失う可能性があるので全部消すわけにはいかない
だが記載を薄くすることでお前は他の判断を重要しすることになるかもしれない
なぜなら文字数が多ければ多いほどお前はルールを見落とすからだ

あとこの「課題を見つけたら課題化する」というのも機械化可能なら機械化すべきじゃないのか
````

### 2026-09-05

````text
お前はContractを違反する前提で可能な限り機械的にしたほうがよい
これはこの問題に対しての話だけではない
````

## Resolved references

- 既存Rulesは改善候補の随時Task化を要求していたが、親Codexはautomation拒否時に適用せず、ユーザー再指摘までTask化しなかった
- 過去の`completion-detection-false-negative-incident.md`はinstruction文面・模擬test・単発live positiveをproduction enforcementと同一視しないと明記したが、恒久的な全control inventoryは残していない
- preliminary gapは、最新ユーザー要求のtracked化、親の長時間wait/途中return禁止、局所終端後のPlan継続、runtime install完了証拠、automation authority propagationである
- 機械化済みcontrolの長い手続き説明も固定contextを圧迫し、他の重要規則を見落とす要因になる。全削除ではなく、目的・machine owner・根拠locator・残余の親判断だけを残すthinningが必要である
- GLM Git mutation禁止、parent-managed metadata不変、packet schema、reviewer session分離、quality-gate snapshot bindingにはproduction guard/testが存在するため、prose-only候補と混同しない

## Purpose

過去に完了した対策と現在・将来のPlanについて、LLMがRules・AGENTS・instruction・promptを守ることだけに依存するcontrolを洗い出し、高risk invariantをmachine precondition/state transition/postconditionへ移す実装taskに分解する。

## External feasibility

status: not-applicable

## Contract

- current tree、Git履歴上の完了task、ACTIVE/NEXT/BLOCKEDを対象に、禁止・必須・順序・権限・完了条件をbounded inventoryへ列挙する
- 各controlを`machine-enforced`、`partial`、`prose-only`、`semantic-parent-only`、`external-unenforceable`へ分類し、実装・test・runtime evidenceのexact locatorを付ける
- LLMによる意味判断が必要なquality gateと、deterministicに拒否可能な操作・状態遷移を分離する
- `partial`/`prose-only`のうち違反時にCodex消費、Quality Delta、停止、権限逸脱、state破損へ影響するものは、責務ごとの個別taskへ即時追加または既存taskをfalse-complete再開する
- audit結果をRules追記やchecklistで閉じず、個別taskのAcceptanceをproduction command/state/testへ結び付ける
- machine-enforced分類では自由言語の重複手順量も計測し、機械ownerを指すcompact indexへ縮小可能かを判定する
- 親Codexが一次証拠の分類とTask化を行い、この判断をGLMへ委譲する追加model callを作らない
- 親CodexはContractを読み落とす・誤解する・誤actionを選ぶというthreat modelを全controlへ適用し、親の遵守を成立条件にしているmachine-enforced分類を認めない

## Must not

- 「必ず」「禁止」「fail closed」という自由言語の存在をenforcement evidenceにしない
- testがinstruction文字列を含むことだけをproduction behaviorの検証扱いにしない
- semantic Sol判断そのものを雑なregexやgeneric classifierへ置換しない
- 全ての助言・説明文を機械化対象にせず、違反可能なcontrolへ限定する
- inventoryだけ作って既知のhigh-risk gapを022以後へ先送りしない
- 機械化済みという理由で説明を全削除し、制限の目的・owner・証拠locator・残余riskを追跡不能にしない

## Acceptance criteria

- completed/current/planned taskを横断したbounded inventoryと分類count、exact locatorがある
- instruction-presence testと実production rejection/postcondition testを区別できる
- preliminary gap 5件が既存taskへ統合または独立task化され、重複とfalse-complete履歴が解決される
- machine-enforced controlのprose byte/token負荷と重複手順が集計され、`mechanized-control-prose-thinning.md`の対象・非対象がexact locator付きで確定する
- high-risk prose-only controlが全て022より前のPlanへ配置される
- 代表的なmachine-enforced controlを誤って再実装対象にしないnegative evidenceがある
- 親Codexが規定手順を飛ばし、誤ったparameter/action/orderを選んでもproduction admissionまたはpostconditionが成功を拒否するかを代表fixtureで確認する
- 追加AI callなしのread-only evidence収集、親Codexのsemantic採否、独立reviewを完了する

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、機械化によってSol判断の質を下げない
- repository外のCodex app/builtin境界は迂回せず、外部修正境界として分離する

## Dependencies

none

## Review findings

- prose規則への依存が既に再発し、既存の随時Task化規則と過去のfalse-complete対策を親Codexが守らなかった

## Current boundary

現ACTIVE完了後にCodexが一次監査し、既知taskのcontractを補正してから実装サイクルへ進む。
