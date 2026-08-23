# Task: Claude CLI compatibility preflightの成立性を検証する

## Original instruction

````text
2026-08-23 user instruction（外部OSS比較調査から本taskに該当する原文）:

### 1. `coredo-eu/codex-claude-orchestrator`

Codexをauthorityとして残し、Claude Codeをpersistent worker runtimeとして利用する実装。

特に参考になったのはClaude CLIとの互換性を
host contractとして明示している点。

実装では、依存するClaude CLI機能について、

- `claude --version`
- `claude --help`
- `--model`
- `--agents`
- `--session-id`
- `--resume`
- `--name`
- `--settings`
- `--setting-sources`
- `--strict-mcp-config`
- `--append-system-prompt-file`
- `--disallowedTools`

等の存在をpreflightで確認する考え方を採っている。

現在の `glm-worker` もClaude Code CLIに強く依存しているが、
Claude依存自体をなくすことは今回の目的ではない。

Claude Codeが提供するagent runtime、tool execution、session/resume、
context管理等を捨ててZ.ai APIを直接使うより、
現時点ではClaude Codeをworker runtimeとして利用し続ける方針。

その前提で、

「Claude CLI仕様変更の影響を早期検出するための
 feature/compatibility preflightが現在不足しているか」

を判断してほしい。

単なるversion pinではなく、
実際に依存するCLI surfaceやprotocolが利用可能かを確認する方法が必要かを見ること。

ただし現行ですでに同等の保証があるなら追加不要。

### A. Claude CLI compatibility preflight

現在のrunnerが依存するClaude CLI flag、
stream-json、structured output、session/resume等について、
Claude Code更新で破壊された場合に、
実taskを走らせる前に分かる十分な仕組みがあるか。

不足しているなら、
最小限のfeature probe / contract testがCodex Reductionと
障害調査コスト削減に値するか。
````

````text
2026-08-23 user follow-up:

じゃあお前の判断でタスク化してくれ
````

## Amendments

none

## Resolved references

- 「お前の判断」は、直前の親Codex調査報告でAを「3. PoCしてから判断」とし、B〜Fは新規task化しないと判断した結果を指す。
- task化対象はClaude CLI compatibility preflightのPoCだけであり、production preflightの採用決定ではない。
- 現行Claude Code 2.1.226ではrunnerの実taskが成立している一方、`--append-system-prompt-file`は`claude --help`上で`--append-system-prompt[-file]`と省略表示され、単純な完全一致検査はfalse rejectになり得る。

## Purpose

Claude Code更新によるCLI contract破壊を実model task・task state mutationより前にAI callなしで検出できる最小preflightが成立するかを検証し、Codexの障害診断・state修復負担を減らす価値があるか判断する。

## Contract

- `internal/runner`が実際に依存するflagと、helpだけでは検証できないstream/result semanticsを棚卸しし、検出可能範囲と非保証範囲を分離する
- `claude --version` / `claude --help`だけを使うno-AI feature checkについて、現行CLI・fake CLI fixtureの両方で成立性を検証する
- helpのgrouped/alias表記を考慮し、単純な文字列完全一致による正常CLIのfalse rejectを避けられるか確認する
- runnerの依存surfaceと検査inventoryのdriftを、単一sourceまたは機械testで防げるか確認する
- binary不在・非実行可能・必須feature欠落を、model callおよびtask/session/checkpoint mutationより前にfail closedできる配置候補を示す
- PoC結果からproduction採用 / 範囲縮小 / 撤退を親Codexが判断し、採用時だけ独立implementation taskへ分離する

## Must not

- 本taskだけでproduction startup、installer、workflow、state semanticsを変更しない
- quota、credential、provider readinessを確認する追加AI callを行わない
- Claude Codeを捨ててZ.ai API直接呼出へ変更しない
- generic doctor/preflight framework、provider abstraction、version pin、daemon、watcherを追加しない
- `--help`で確認できないstream event/result semanticsまで保証済みと扱わない
- help parserのために一般CLI parserや高複雑度の互換layerを追加しない
- 現在のACTIVE taskを切り替えない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- runnerがproductionで渡すClaude CLI flag / modeと、helpで検証可能・不能な依存を再現可能なartifactへ列挙
- Claude Code 2.1.226のno-AI checkがPASSし、`--append-system-prompt[-file]`表記を誤拒否しない
- 必須featureを1件ずつ欠落させたfixtureが、欠落featureを特定してfail closedになる
- binary不在・非実行可能・help失敗の境界を固定
- model call 0件、repository/task/session/checkpoint mutation 0件を確認
- runner依存surface追加時に検査inventory更新漏れを検出できる最小test案またはPoCを提示
- 実行overheadを測定し、通常task前に置く価値があるかをCodex Reduction / Quality Delta / false reject riskで評価
- production採用、範囲縮小、撤退のいずれかを親Codexが証拠付きで決定
- 必要最小限のtest、独立reviewer、risk/contractに応じて必要なSol品質gate、親Codex commit。production変更を採用する場合は別task

## Historical invariants

- `install.sh`は現在、Claude binaryがPATH上にある場合だけ`--json-schema`を検査し、binary不在時はruntime構成へ委ねてskipする
- production runnerはClaude Code CLI 2.1.226、stream-json、role別`--json-schema`、session/resume、隔離settings、strict child-env allowlistで成立済み
- structured output PoCとproduction migrationは完了済みであり、本taskで再実装しない
- provider recovery probeはAI callを伴う障害回復用であり、通常task前のcompatibility checkへ流用しない

## Dependencies

none

## Review findings

none

## Current boundary

未着手。外部OSS比較によりpreflight範囲の不足可能性を認識したが、現行CLIでの障害は発生しておらずproduction採用価値は未確定。
