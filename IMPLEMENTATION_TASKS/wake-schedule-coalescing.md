# Task: Codex wakeとGLM resume wakeの重複発火を防止する

## Original instruction

````text
許可はする
だが二重に作ることはTokenの無駄じゃないのか
````

````text
二重発火を今後も防げるようになってるのか？
````

````text
じゃあタスクに積むなり今やるなりしろ
````

## Amendments

none

## Resolved references

- 「二重」は、親Codexの5h reset後に親実装taskへ`作業を続けろ`を送る専用Codex wakeと、GLM 5h limit後に同じ親実装taskで`glm-worker --resume`を行うGLM resume heartbeatが近接時刻に別々に発火する状態を指す。
- 2026-08-26の実例ではGLM自動再開候補が00:17:55 JST、既存Codex wakeが00:20:59 JSTで約3分差だったため、親Codexが手判断でGLM専用wakeを作らずCodex wakeへ統合した。
- 現時点の防止は会話context上の手判断だけであり、Compaction後も保証するcanonical contractやdeterministic gateは未実装である。

## External feasibility

status: not-applicable

## Purpose

既存Codex wakeでGLM rate-limit taskも十分早く再開できる場合に、同じ親taskへ近接するGLM wakeを追加してCodex turn/tokenを二重消費することを防ぐ。

## Contract

- GLM 5h rate-limit packet受理時に、現在の親実装taskへ再開指示を送るACTIVEなCodex wakeの有無・対象thread・次回絶対時刻を機械確認する。
- 既存Codex wakeがGLM resume時刻直後の許容範囲で確実に発火し、同じ保存taskを再開できる場合はGLM wakeを作成しない。
- 既存wakeが無い、対象thread不一致、PAUSED/不正、時刻が早すぎる・遅すぎる、検証不能の場合だけ既存GLM auto-resume contractへ進む。
- coalesceした場合もCodex wake受領後に`glm-worker --status`のtask ID/status/resume可否を一度検証し、一致時だけ同じcheckpointからresumeする。
- 発火済み・終端済みautomationの増殖、複数予約、固定間隔pollingを行わない。
- 許容時間境界はtoken節約と不要な開発停止時間のtradeoffを一次証拠から決め、自然言語の「近い」だけに依存しない。

## Must not

- Codex wakeとGLM wakeを常に両方作らない。
- 会話memory、親Codexの都度判断、automation名だけを重複防止の唯一の根拠にしない。
- Codex wakeが大幅に遅い場合までcoalesceしてGLM再開を長時間遅延させない。
- Codex 5h wakeとGLM resumeの責務・session ownershipを統合しない。
- 汎用scheduler framework、polling、queue、複数未処理runを追加しない。

## Acceptance criteria

- 既存Codex wakeが同一thread・ACTIVE・許容時刻内ならGLM wakeを新規作成しない。
- wake不存在、別thread、PAUSED、不正schedule、許容時刻外、検証不能ではGLM wake作成経路を維持する。
- coalesce後のCodex wakeでtask ID/status/resume可否不一致ならresumeしない。
- 同一rate-limit eventで二つのCodex turnが発火しないことをdeterministic test/scenarioで固定する。
- token節約量と最大追加待ち時間の境界を記録する。
- 既存Codex wake・GLM auto-resume・rate-limit checkpoint/session保持を壊さない。
- 関連test、全必要gate、独立review、必要なSol gate、親commit・remote main fast-forward、本配置を完了する。

## Historical invariants

- Codex wakeは専用session、GLM resume heartbeatは親実装sessionが所有する。
- Codex wakeとGLM wakeの個別責務、1 session 1 scheduler、固定間隔polling禁止を維持する。

## Dependencies

none

## Review findings

none

## Current boundary

NEXT。現在ACTIVEのmachine output boundary task完了後、EVAL責務整理より先に実装する。
