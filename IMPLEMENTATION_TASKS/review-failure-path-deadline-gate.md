# Task: 外部model呼出のbounded deadline gate

## Original instruction

```text
2026-09-28 Sol decision: System-One review feedback評価で確認したF3（外部model invocationのbounded deadline欠如）を、実行経路を限定したdeterministic gateとして実装する。deadlineのない対象呼出を機械的に検出し、Solが同じfindingを繰り返し発見する負荷を減らす。
```

## Amendments

none

## Purpose

外部model呼出に有効な終了期限がないという反復可能なfailure classを、review前の機械検証で捕捉する。

## External feasibility

status: not-applicable

## Contract

- current codeの外部model呼出と期限付き実行APIを確認し、静的に証明できる狭い構造だけをgate対象にする。
- timeout値の定義だけで成功とせず、対象process実行へdeadlineが伝播することを検証する。
- deadline欠落の代表例が失敗し、期限付き経路と対象外の通常process実行が誤検知されないtestを設ける。
- gate導入前後の対象経路と反復Sol review findingへの効果を記録し、Codex Reduction候補として評価可能にする。

## Must not

- 任意の`exec.Cmd`や無関係なprocess実行へ一般化しない。
- deadlineの存在だけを理由にprovider failureを成功扱いしない。
- reviewer構成、routing、publication経路を変更しない。

## Acceptance criteria

- F3型のdeadline欠落をgateが検出し、既存の期限付き呼出を通す。
- 対象範囲とfalse positive境界をtestで固定する。
- 通常のreview、validation、publicationを経て1 Task = 1 commitでterminalとなる。

## Historical invariants

- Sol Highは曖昧・高riskなsemantic tailに集中させ、明確なfailure classは機械へ移す。
- Codex ReductionとQuality Deltaを最上位Evalとする。

## Dependencies

none
