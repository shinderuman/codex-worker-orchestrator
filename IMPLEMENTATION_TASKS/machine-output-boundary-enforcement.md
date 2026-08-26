# Task: 外部machine output contractを共通boundaryで強制する

## Original instruction

````text
TASK

外部machine output contract違反を、個別prompt遵守やfinal review検出ではなく機械的に防止する。

CURRENT STATE

- `glm-worker --install-smoke` で、内部shell smokeのstdout/stderrをcommand自身へ直結し、人間向けtext + JSONを外部へ混在させる回帰が発生した。
- 親Codex final reviewで検出し、現在GLMへ個別修正を指示済み。
- 既存contract:
  - single-shot success: stdoutは単一JSON object
  - single-shot failure: structured process error JSON + non-zero exit
  - stream modeのみJSONL
- 今回の個別regression testだけでは、将来の別commandで同種違反を再発できる。

DO

- 現在の外部command出力経路を棚卸しし、single-shot / streamの分類とstdout/stderr ownershipを確認する。
- machine commandから外部stdoutへ出力できる経路を最小の共通boundaryへ集約する。
- single-shot commandでは、内部処理・subprocess・shellのstdout/stderrを外部machine stdoutへ直接接続できない構造にする。
- subprocess等の診断出力が必要ならcaptureし、既存artifact/state等の適切な境界へ保存し、必要時のみstructured JSONから参照する。
- 外部stdoutの最終serializationは既存machine contractを所有する単一boundaryだけが行う構造を優先する。
- 以下のような直接漏出経路をrepo全体で監査する。
  - `cmd.Stdout = os.Stdout`
  - `cmd.Stderr = os.Stderr`
  - `os.Stdout.Write`
  - machine command pathからの直接print
  - shell/subprocess stdout/stderr継承
- 機械的に禁止可能ならlint/static test/architecture test等の最小方式で禁止する。
- 全外部commandについて、少なくとも以下を自動検証できる共通contract testを作る。
  - single-shot successのstdoutはexactly one JSON object
  - trailing text/second JSON/JSONL/text混在なし
  - single-shot failureはstructured JSON + non-zero exit
  - stream commandだけJSONLを許可
- 新規command追加時にも、そのcommandを個別に思い出してtest追加しないと漏れる設計を避ける。既存command registry/schema/dispatch等から列挙できるなら再利用する。
- 今回の`--install-smoke`個別regression testは維持し、一般invariant testと役割を分ける。

DO NOT

- promptへ「stdoutへtextを出すな」を追加するだけで完了しない。
- 親Codex final reviewを主たる防止機構にしない。
- GLMの自己判断・注意力を品質保証として使わない。
- 新しいgeneric logging frameworkを作らない。
- 新しいstream modeや互換fallbackを追加しない。
- machine JSON contractそのものを緩めない。
- subprocess診断textをstderrへ移すだけで解決扱いにしない。外部machine boundaryとして許可されていないtextは漏らさない。
- unrelated command architectureを大規模rewriteしない。

ACCEPTANCE

- `--install-smoke`以外の将来commandでも同種のtext/JSON混在を機械的に防止できる。
- single-shot commandからserializer以外を経由してmachine stdoutへ任意textを出す経路が存在しない、またはdeterministic gateで拒否される。
- subprocess/shell stdout/stderr直結による漏出をtestで再現し、失敗することを確認できる。
- single-shot success/failure、streamの各contractを共通testで固定する。
- 新規single-shot commandを追加して不正なstdout直結を行った場合、review前のmachine gateで失敗する。
- 既存machine output contract・exit semantics・artifact/state境界を維持する。
- 関連test、全必要gate、独立reviewを通す。

BOUNDARY

- 現在進行中の`--install-smoke`個別fixは中断しない。
- 本件はescaped bugの再発クラスを潰す独立follow-up taskとして保持する。
- 個別bug修正完了後、同種違反を「GLMがルールを守ること」ではなく「仕組み上作れないこと」へ収束させる。
````

## Amendments

none

## Resolved references

- 「現在進行中の`--install-smoke`個別fix」は`IMPLEMENTATION_TASKS/full-smoke-evidence-reuse.md`の親Codex final review差戻しを指す。
- 「既存contract」はTask 007で統一済みのglm-worker外部JSON/JSONL machine contractを指す。

## External feasibility

status: not-applicable

## Purpose

外部machine outputの単一JSON/JSONL contractを共通のproduction boundaryとdeterministic gateで強制し、新規commandでsubprocess textが混入する同種回帰をreview前に防止する。

## Contract

- 全外部commandをsingle-shot/streamへ分類し、stdout/stderr ownershipとproducer経路を一次証拠で監査する。
- single-shotの最終stdout serializationを最小の共通boundaryへ集約し、内部処理・subprocessの任意text直結を構造またはdeterministic architecture gateで拒否する。
- successはstdout exactly one JSON object、failureはstructured process error JSON + non-zero、streamだけJSONLを維持する。
- 診断textは外部stdout/stderrへ漏らさず、必要な場合だけ既存artifact/stateへ保存しstructured参照を返す。
- command registry/dispatch等から全commandを列挙して共通contract testへ接続し、新規command追加時の手作業test登録漏れを防ぐ。

## Must not

- prompt遵守、GLM注意力、親final reviewだけを防止機構にしない。
- machine contractを緩めず、stderrへのtext移動、dual output、新stream/fallbackで回避しない。
- generic logging frameworkやunrelated command architectureの大規模rewriteを追加しない。

## Acceptance criteria

- 全single-shot success/failureとstreamの外部contractを共通testで固定する。
- serializer以外の任意text stdout、subprocess stdout/stderr直結、trailing text、second JSONがdeterministic gateで失敗する。
- command追加時に共通検証対象から黙って漏れない。
- `--install-smoke`個別regression testと一般invariant testの両方を維持する。
- 既存exit semantics・artifact/state境界を維持し、関連test、全必要gate、独立review、親最終採否を通す。

## Historical invariants

- Task 007の外部JSON/JSONL単一contractを維持する。
- full smoke証拠再利用のidentity・失効・ledger契約を変更しない。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。full smoke証拠再利用taskの個別machine output fix・品質gate・親commit完了後、全command共通のmachine output強制境界を設計・実装する。
