# Task: GLMが読むinstruction surfaceの自己改変を防止する

## Original instruction

````text
F1: AGENTS.local.md が自己改変instruction channelになり得る

- worker/reviewerはrepoの`AGENTS.local.md`を読む。
- parent-managed metadata guard対象外。
- self-protectionのknown critical pathにも含まれず、unknown pathとしてLOWになり得る。
- GLM自身が作成・変更したinstructionを次回以降のGLMが読む構造を許している。

REQUIRED HARDENING

- repo-local instruction surfaceを棚卸しする。
- GLMが読むinstruction fileをGLM自身が変更可能な状態にしない。
- `AGENTS.local.md`を許可するならownership / mutation guard / snapshot guardを機械強制する。
- GLM呼出中だけでなくtask全体の適切な境界で自己改変instructionが次回sessionへ持ち越されないことを保証する。
- 「promptに編集禁止と書く」だけは禁止。
````

## Amendments

- 2026-08-26 Product boundary: `AGENTS.local.md`固有対策だけで終わらず、任意repositoryでglm-workerが実際に読むinstruction surfaceをworker自身が次回向けに改変できないgeneric ownership境界をTrack Aとして実装する。
- 2026-08-26 Clarification: Track Aのgeneric production improvementとTrack Bの本repository hardeningを区別して両方評価する。Aで成立しないことを理由にBとして必要な保全を捨てず、同一mechanismで双方を満たせる場合は重複実装しない。

## Resolved references

- Track Aは任意repositoryのglm-worker production invariant、Track Bはcodex-worker-orchestrator固有hardeningを指す。

## External feasibility

status: not-applicable

## Purpose

GLMが読むinstructionをGLM自身が改変して次回sessionの要求・権限を変えるfailure classを、generic ownershipとrepo固有guardの必要境界で機械防止する。

## Contract

- instruction discovery/read graphを任意repoと本repoで分けて棚卸しし、F1をTrack A/B/既存包含/不要へ一次証拠で分類する。
- worker/reviewer task全体をまたぐinstruction identity・ownership・mutation postconditionを機械強制し、親Codex authorityと通常source editを阻害しない。
- repo固有path追加だけをTrack A成果に数えない。

## Must not

- prompt禁止だけ、全file immutable、全変更HIGHで解決しない。
- Track A/Bを混同しない。

## Acceptance criteria

- GLMが読むinstruction surfaceの自己改変が次回sessionへ持ち越されないproduction-path testを持つ。
- F1のTrack A/B分類と実装・非実装理由を記録し、必要な両trackを完了する。
- 親権限・外部repoの通常編集・Codex/Sol cost境界を維持する。

## Historical invariants

- machine output boundary taskとEVAL責務整理の成果を変更しない。

## Dependencies

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`

## Review findings

none

## Current boundary

NEXT。EVAL責務整理完了後にF1をTrack A/Bへ分類して着手する。
