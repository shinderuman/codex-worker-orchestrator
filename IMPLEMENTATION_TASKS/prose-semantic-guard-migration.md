# Task: substring prose pinをbehavioral/typed guardへ移行する

## Original instruction

````text
F7: prose contract testがsubstring存在確認だけの箇所がある

- 必須語句が存在することだけを`strings.Contains`等で固定しているtestがある。
- GLMが同じsectionへ例外文・但書・逆条件を追加して意味を弱めてもtestが通る可能性がある。
- prompt proseをmachine invariantとして扱っている箇所全体を監査する必要がある。

REQUIRED HARDENING

- prompt/instruction testでsubstring存在だけをmachine contract代替にしている箇所を棚卸しする。
- proseの意味変更を本当にmachine invariantとして守る必要があるものは、可能ならtyped config / enum / structured contract / behavior testへ移す。
- prose自体が正本の場合も、単語存在だけで反対意味の但書を許す脆弱なtestを主要guardにしない。
- exact全文固定でMarkdownをappend-only化する解決も禁止。
````

## Amendments

- 2026-08-26 Product boundary: substring test追加だけをTrack A成果にせず、generic behavior/typed invariantへ移せるものを評価する。repo固有prose guardはTrack Bとして別計上する。
- 2026-08-26 Clarification: F7をA/B両方で評価し、実装容易性を理由に間引かない。

## Resolved references

- prose pinはprompt/instruction/scenario/testの`strings.Contains`、grep等による必須語句存在確認を指す。

## External feasibility

status: not-applicable

## Purpose

反対意味の但書を追加しても通るsubstring pinを主要machine invariantから外し、behavior/typed contractへ収束する。

## Contract

- EVAL整理後にprose pin corpusを棚卸しし、Track A behavior invariantとTrack B repo固有正本を分類する。
- exact全文固定を避け、重要semanticだけをdeterministicに検証する。

## Must not

- substring追加、全文golden、Markdown append-only化だけで完了しない。

## Acceptance criteria

- 逆条件・例外文で意味を弱める代表caseが主要guardを通らない。
- prose pin総数とbehavior/typed移行、残存理由を報告する。
- F7のA/B分類を記録する。

## Historical invariants

- lossless requirement sourceとEVAL coverageを維持する。

## Dependencies

none

## Review findings

none

## Current boundary

NEXT / NARROW。PR #4でscenario authority、prose存在確認test、historical protocol corpus等を大量に削除し、behavior / typed / machine gateへ移したため元scopeは縮小した。ただし現mainにも`activetask_test.go`等でpromptの`ACTIVE_TASK_FILE`、`Acceptance criteria`、`task file本文`等を`strings.Contains`で確認するprose pinが残る。残存corpusを棚卸しし、主要semanticをbehavior / typed guardへ移せるかと正当な残存理由を分類するscopeで継続する。
