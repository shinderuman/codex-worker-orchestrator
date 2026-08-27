# Task: 要求結果と確定済み設計判断を区別してSol gateを強制する

## Original instruction

````text
F6: Sol判断不要例外の適用範囲が広い

- ACTIVE task等で方向確定済みなら、新type/package/interface等でもSolへ戻さない。
- 「互換性を狭めず強化」「明白な仕様準拠」等も自律判断対象。
- taskが結果だけを指定していて設計軸までは確定していない場合でも、「方向確定」と解釈して新責務やvalidation強化へ進む余地がある。

REQUIRED HARDENING

- 「方向確定済み」の意味を、taskが結果を要求しているだけの場合と、設計軸まで明示確定している場合で区別する。
- 新責務、依存方向、公開surface、互換意味、validation意味等が未確定ならSol gateを維持する。
- type/package/interfaceの存在だけでHIGHにするのではなく、意味責務の新設かどうかで判定する。
- 「互換性を狭めず強化」を理由にvalidation/error behaviorを勝手に強化できないようにする。
````

## Amendments

- 2026-08-26 Product boundary: 任意taskで「要求された結果」と「確定済み設計判断」を区別し、未確定な意味判断をGLMが勝手に確定しないproduction behaviorとしてTrack Aで実装する。
- 2026-08-26 Clarification: 本repo固有quality gateはTrack Bとして区別し、A/B両方を評価する。

## Resolved references

- 意味判断は新責務、依存方向、公開surface、互換性、validation/error semanticsを含む。

## External feasibility

status: not-applicable

## Purpose

結果要求だけを設計軸確定と拡大解釈してGLMが高レバレッジ判断を代行するfailure classを減らす。

## Contract

- task requirementから確定済み設計軸と未確定な結果要求を区別する最小typed/structured production boundaryを設計する。
- surfaceの存在ではなく意味責務変更でSol gateを維持し、不要な差戻しも増やさない。

## Must not

- 全type/package/interfaceをHIGH化せず、prompt解釈だけで完了しない。

## Acceptance criteria

- 結果のみ指定・設計軸明示・明白な実装詳細のpositive/negative caseでSol gate境界が決定論的に分かれる。
- validation/error behaviorの無断強化を防ぎ、不要Sol callの増分を報告する。
- F6のA/B分類を記録する。

## Historical invariants

- Sol High品質判断とCodex/Sol削減の最上位目的を維持する。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。latest integrationを履歴を書き換えず同期済み。現行Sol gate・risk classification・task requirement surfaceを一次証拠で確認し、F6 Track A/B分類とproduction boundaryを確定して実装を進める。
