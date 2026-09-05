# Task: Parent Codex rollout chain attribution

## Original instruction

````text
post-105-codex-efficiency-reevaluation.mdを定期的にやってほしいんだが奥に追いやられて一向に行われないので同様のタスクをなるべく早いタイミングでやってくれ
いまのpost-105-codex-efficiency-reevaluation.md自体は022の直前に残す
だが現段階の再評価をしてほしい
そして再評価したあとにタスクの優先度つけ直しをしてほしい
````

## Amendments

none

## Resolved references

- 2026-09-03T00:48:25Z開始の`~/.codex/sessions/2026/09/03/rollout-2026-09-03T09-44-55-01a064b9-d537-78e2-87c6-49709e1aa697.jsonl`と、2026-09-05T03:45:34Z開始の`~/.codex/sessions/2026/09/05/rollout-2026-09-05T12-45-34-01a064b9-d537-78e2-87c6-49709e1aa697_01a06fab-f155-7092-bc8a-44c724514f95.jsonl`は同じsession metadata ID、repository cwd、source、originatorを持ち、時間範囲が重ならない
- 2026-09-03 JST以降24 taskの`--parent-usage`は、前半4 taskだけ`included`、rollout分割後の20 taskを`2 rollouts share the stored parent thread ID`として`ambiguous`にした
- 2本のrolloutで観測したCodex cumulative totalはそれぞれ17,047,913 tokensと5,733,188 tokensだが、現行task attributionは分割後のtaskへ利用できない

## Purpose

Codex Desktopが同じthreadを複数rollout fileへ正規分割した後も、重複や別sessionを誤結合せずtask別Codex実消費を取得し、Direct Codex対Codex + glm-workerの最上位Evalを継続可能にする。

## External feasibility

status: not-applicable

## Contract

- stored parent identityと各rolloutのsession metadataを正として、同じthread ID・repository cwd・source・originatorを持つ複数fileをordered rollout chain候補として扱う
- event timestamp範囲が重ならず、各file内のtoken counterが自己完結している正規分割だけをchainとして受理し、task intervalと交差するfile segmentごとにcounter anchor差分を計算して重複なく合算する
- file順序、採用/除外理由、各token anchor、activity count、source fileと1-based lineをbounded evidenceへ返す
- overlapping range、同一内容duplicate、identity/cwd/source/originator不一致、counter不整合、読取不能は推測結合せずambiguous/unknown/unreadableを維持する
- `--parent-usage`、bundle `analysis-index.json`、parent finalization/subsequent request partitionが同じchain resolverを共有する
- current単一rolloutとarchived rolloutの既存結果を変更せず、model call・rollout rewrite・新規DBを行わない

## Must not

- filename suffix、mtime、近接時刻だけで別fileを同一chainと推定しない
- 複数fileのcumulative counterをそのまま加算して境界tokenを二重計上しない
- overlapping/duplicate fileを都合よく片方だけ選びcomplete coverageにしない
- attribution不能を0 tokenまたは改善値として扱わない

## Acceptance criteria

- current sessions内の正規2分割、current+archived分割、3分割、taskがfile境界を跨ぐfixtureでtoken/activity/compaction/tool bytesが重複なく一致する
- duplicate、overlap、identity mismatch、counter reset anomaly、unreadable fileは理由付きでfail closedする
- 2026-09-05実dataで期間内24 taskのうちrollout分割だけを理由にambiguousだった20 taskが安全に解決されるか、残る個別ambiguity理由へ狭まる
- `--parent-usage`とbundle analysis-indexが同じsource locator・interval partitionを返す
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- attribution不能値、counter reset、複数rollout候補を便宜的に合算しない
- SessionIDはrun identityであり、task IDやfilenameから推測しない

## Dependencies

none

## Review findings

- 現行resolverは正規rollout分割と競合duplicateを区別せず、候補2件以上を一律ambiguousにするため、継続sessionのCodex実消費coverageが4/24 taskへ低下した

## Current boundary

品質toolchain recovery完了後、telemetry history compact summaryより先に実装する。
