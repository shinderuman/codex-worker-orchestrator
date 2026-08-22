# Task: parent review outcomeを低cardinality telemetryとして観測する

## Original instruction

````text
2026-08-23 parent Codex derived instruction:

複数repositoryの保存済みtelemetryを計測した結果、glm-workerは`fix_commands`を数えているが、そのfixがCodex最終reviewの差し戻し、ユーザー追加指示、external review対応、metadata修復のどれかを区別できない。また、親Codexがterminal resultを修正なしで採用した事実もglm-worker側telemetryだけでは確定できない。

Kimi K3を含むreview追加・縮小・model routingの効果を推測で判断しないため、親review機会と結果をraw本文なしの低cardinality telemetryで測定可能にする。既存のworker/reviewer call telemetryと対応付け、Codex自身の差し戻し率・再work量を他のfix sourceと分離する。追加AI callやraw instruction保存は行わない。
````

## Amendments

none

## Resolved references

- `fix_commands=11`は2026-08-23のcodex-config `glm-worker --stats`で観測した値。7 task historyに分布するが、originは既存schemaから確定できない。
- `親review`はglm-worker terminal resultを受け取った後に親Codexが行う採用・差し戻し・判断・中止を指し、GLM reviewer内部の`FIX_REQUIRED` / auto-fixとは別である。

## Purpose

Codex最終reviewの差し戻しと他source由来のreworkを分離し、最上位EvalのCodex ReductionとQuality Deltaを補助する親touchpoint / rework観測を可能にする。

## Contract

- parent review opportunityと、accepted / parent-fix / user-amendment / external-review-followup / metadata-repair / decision / abandoned-or-unknown等の低cardinality outcomeを、実際に確定できる境界だけで記録する
- 既存task / phase / role / model / risk / call telemetryとdeterministicに対応付け、親fixとreviewer auto-fixを分離する
- outcome未記録や旧taskをacceptedへ推測補完せずunknownとして扱う
- retry、resume、同じdecision/fixの再実行で二重計上しないexactly-once境界を定義する
- repository別にCodex差し戻し率、差し戻し後worker/reviewer call・turn・token・duration増分を追加AI callなしで集計可能にする
- parent outcomeはCodex actual token usageそのものではないことをschema・表示で明示し、Direct/orchestrated A/Bの代替metricにしない

## Must not

- raw fix / decision / user instruction / prompt / response本文、path本文、秘密、高cardinality labelを新規保存しない
- parent reviewの意味契約をGLM modelに推定させない。分類のためのAI callを追加しない
- 新task開始、task status、`NEEDS_SOL_REVIEW`、`--fix`単独をacceptedやCodex差し戻しへfail-open推定しない
- telemetry導入だけでreviewer省略・model downgrade・Kimi K3導入を決定しない
- parent action回数削減だけでQuality Delta維持やCodex token削減を達成したと判定しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- parent review opportunity / outcome / originの有限集合と記録主体・確定境界をartifactで定義
- accepted、Codex差し戻し、user amendment、external review、metadata repair、decision、unknown、retry/resumeをproduction-path testで固定
- 既存`fix_commands`、reviewer `FIX_REQUIRED`、auto-fix、terminal packetとの加法整合
- 旧telemetryはunknownを維持しmigration・本文解析で補完しない
- repository / task / risk / model別のread-only集計と、差し戻し後追加消費を再現可能に表示
- Task 101のDirect/orchestrated A/Bとは補助観測の関係に留め、actual usage / quality artifactと混同しない
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じたSol品質gate、親Codex commit、本配置

## Historical invariants

- model call telemetry exact-once、TaskStats加法整合、raw content privacy
- Task 009 call outlier、Task 013 model routing evaluation、Task 106 review/fix call縮小は本taskの観測結果を利用できる

## Dependencies

none

## Review findings

none

## Current boundary

未着手。現在は`fix_commands`総数だけがあり、parent review outcomeとfix originを直接観測できない。
