# Task: Codex / glm-worker / Claude CLI通信経路を見直し、原則統一する

## Original instruction

````text
Bundle生成の見直しというタスクがあるんだがそれ以降のどこかにいまの通信方式が統一されていないことを見直す、基本的には統一するというタスクを入れるPR作ってほしい
````

## Amendments

### 2026-10-25 External Reviewから同一責務へ統合: controller-execution-typed-staging-coverage

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
合法なquiescent execution操作とtermination reason slotをcanonical typed commandへ接続する。
suspend/materialize/cleanup/gc-suspensionとuser-directed terminationでhandoff→prepare→consumeの対応を実経路から整理する。required controller-executionでcommand欠落となる正当状態をcanonical operation ownerで修復する。
execution_termination.reasonとruntime_disposition.reasonを必要なmachine-declared slotに限定して扱い、user-directed terminationの意味判断を自動生成しない。
generic placeholderや自由JSONによるauthority補完へ戻さず、stale generation/改変command/不明slotを拒否する。
````

## Resolved references

- 「Bundle生成の見直し」: `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`。三つのBundleから収録内容と生成時間を再監査するTask。依存の扱いは `Fulfilled dependencies`、現在の実行順はPlanを参照する。
- Parent action: `glm-worker/internal/parentactioncmd/parentactioncmd.go`、`glm-worker/internal/parentaction/parentaction.go`。semantic payloadをtoken-bound staging fileからconsumeし、`glm-worker`へstdinで渡す。
- Worker/Reviewer: `glm-worker/internal/runner/runner.go`。Claude CLI起動時にpromptをargvへ載せ、session resumeとstream-jsonを使う。
- 単発Decision: `glm-worker/internal/runner/decision.go`。stdinでpromptを渡す。現行の呼出元・利用目的は実装から再確認する。
- TTY: `glm-worker/internal/app/execution.go`、`stdin_terminal.go`、`stdin_terminal_raw_unix.go`。PTY raw modeとstdin_readyが残るが、通常のparent-action内部Pipeでは使わない。

- PR #1390 / comment 4370316231: consistent controller-executionをquiescent ownerがtyped commandなしで返す経路がある一方、stagingはcommand必須。generic placeholder fallbackの復活は採らず、合法operationのtyped projectionとstagingの閉包を修復する。termination reason slotも同じtransport境界の未対応として含める。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/app/parent_handoff_controller.go:applyCanonicalQuiescentHandoff`
- `glm-worker/internal/app/parent_handoff_controller.go:canonicalQuiescentAction`
- `glm-worker/internal/app/controller_execution_projection.go:runControllerExecutionProjection`
- `glm-worker/internal/parentactioncmd/execution_staging.go:executionStagedTemplate`

## Purpose

Codex→parent action→glm-worker→Claude CLIの通信経路を全体として調査し、同等の指示・prompt・payload輸送を原則一つのcanonical方式に統一する。旧方式を互換目的で維持して保守面積・失敗経路を増やさない。

## External feasibility

status: not-applicable

## Contract

- 親start/decision/fix/milestone/accept/resume、通常Worker、独立Reviewer、単発Decision、停止・再開・recoveryの呼出元と入出力を追跡する。argv、stdin/pipe、PTY、staging file、framing、session、schema、error処理を現行コードと実際の利用状況に照合する。
- 同等のpayload/prompt伝送方式は**原則として単一のcanonical実装へ統一**し、不要となった重複経路・adapter・関連test・instructionを撤去する。異なる方式を残す場合は、実際の機能要件や制約による必要性を示す。歴史的経緯や「recovery/debug用に使える」だけを恒久存続理由にしない。
- 特に通常Worker/Reviewerのprompt argvと単発Decisionのprompt stdinの差を精査し、session resume・tool-use・structured outputを維持できるなら共通方式に統一する。長文promptがOSのargv長制限へ依存する構造を必要性から見直す。
- Parent actionのtoken-bound stagingと内部stdinは現役機能として扱う。直接PTY raw/READY経路に正規の利用者がいなければ除去する。内部で実際に使う`--decision-stdin`/`--fix-stdin`を、PTYと一括で旧互換扱いして削除しない。
- 方式選択をLLMの都度判断へ戻さず、機械側で一貫して扱う。現行のsemantic authority、Task要求正本、権限分離、session管理、結果schema、停止・再開、fail-closedを保護する。
- 長文・UTF-8・改行・引用符・backtick・dollar・NUL等のpayload完全性、session再開、output JSON/JSONL、timeout、signal、error伝播を該当呼出経路で確認する。

External Reviewで確認した同一責務の具体条件（controller-execution-typed-staging-coverage）:

- suspend/materialize/cleanup/gc-suspensionとuser-directed terminationでhandoff→prepare→consumeの対応を実経路から整理する。required controller-executionでcommand欠落となる正当状態をcanonical operation ownerで修復する。
- execution_termination.reasonとruntime_disposition.reasonを必要なmachine-declared slotに限定して扱い、user-directed terminationの意味判断を自動生成しない。
- generic placeholderや自由JSONによるauthority補完へ戻さず、stale generation/改変command/不明slotを拒否する。

## Must not

- 旧parser、alias、migration、fallback、PTY互換、dual protocol、transport選択オプションを「一応使える」ために維持・新設しない。汎用transport frameworkを作らない。
- Worker/Reviewer/単発Decisionのsemantic機能・ツール許可・セッション性・品質基準まで統一しない。通信の共通化と役割の同一化を混同しない。
- ACTIVE Taskへの介入、Bundle Taskの仕様変更、無関係な実装・Git操作、既存成果の破棄をしない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

1. 現行の通信境界・呼出元とそれぞれの存続理由を、実コードと検証証拠で説明できる。
2. 等価な通信経路は原則統一され、不要なPTY経路・古いadapter・test・instructionが残らない。残る差異には検証済みの現行要件がある。
3. 通常Worker/Reviewer、単発Decision、親decision/fix、resume/recoveryにおいて要求本文の完全性、session継続、構造化出力、エラー時fail-closedが保たれる。
4. Repository Lint、関連test、適切な正規validation、独立reviewを実施し、semantic結果と変更範囲を確認する。

- 正当な各操作は実handoffとstagingを通過し、command/slot欠落で停止しない。
- 否定例はcontroller stateを変更せず拒否し、未知commandを親の推測で実行できない。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- `IMPLEMENTATION_RULES.md`のmachine-only data原則。旧schemaやtransportの後方互換性を保持しない。
- 親Codexはsemantic actionとpayloadを判断し、通信・handshakeは機械が処理する。ACTIVE Taskのauthorityは維持する。

## Dependencies

none


## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`
