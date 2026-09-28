# Task: observationの実行capability deadlock解消

## Original instruction

System-Oneのobservation Taskがread-only workerのcapability不足で停止した。production mutation禁止を維持しつつ、許可された実測とvalidationをmachine-owned executorへrouteする。元Taskはparkしたまま保持し、完了後に復帰する。

原文の保管済みTask file SHA-256: c8b43bbb533dd8772c9da6f1f8da0c24e1971bc46edeee581f4a35fe7baf98f5

## Amendments

なし。

## Resolved references

元Task: `IMPLEMENTATION_TASKS/system-one-adoption-evidence-gate.md`。実装成果: `eb35be276965418c714236e080d8146f80e09659`。

## Purpose

606 TaskのTask historyをcompletion guardへ提供する。

## External feasibility

status: not-applicable

## Contract

606のsource実装commitを変更しない。Task history補正だけを記録する。

## Must not

production sourceの追加修正、別Task・別worktreeの作成。

## Acceptance criteria

606のcompletion後、元System-One Taskへ復帰できること。

## Dependencies

Outstanding: none
