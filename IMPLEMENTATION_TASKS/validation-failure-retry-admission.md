# Task: 同じ失敗根拠での長時間Go Gate再実行を防ぐ

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「IMPLEMENTATION_PLANは既存含め優先度を考え直す」。

## Resolved references

同じsource・古いinstalled CLIでGo FAILを同一blocking facade内に二回投入（#1346 F06、追加約642秒）。同じsource/timeoutの約20分Go FAILを二回（#1356 F02）、後続類例（#1369 M03）。現行開放中のCI高速化PR #1385、Go版整合/速度PR #1389とは実装ownerを重複させない。

- PR #1368 / comment 4326838848: PrepareFinalValidationWorkspaceが未宣言formで先にplain errorを返し、routingのstructured blockedへ到達しない。validationを実行せず所定のblocked reasonへ収束させる。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/controller/publication_validation_workspace.go:PrepareFinalValidationWorkspace`
- `glm-worker/internal/parentactioncmd/finalization.go:runFinalizationCheck`

## Amendments

### 2026-10-25 External Reviewから同一責務へ統合: finalization-undeclared-form-routing

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
未宣言formを既定のstructured blockedへ収束させる。
PrepareFinalValidationWorkspaceの準備段階とfinalizationRoutingDecisionを接続し、requested formがない場合はvalidation/checkoutを起動せず既定reasonを返す。
破損candidate/別repository/未知formをno obligationへ握り潰さず、実controller StateBaseを使う正規入口を検証する。
````

## Purpose

Gate admissionはsource OID/tree、installed tool SHA、argv/timeout、環境・Snapshot、既存失敗原因と再投入の新情報を照合する。

## External feasibility

status: not-applicable

## Contract

Gate admissionはsource OID/tree、installed tool SHA、argv/timeout、環境・Snapshot、既存失敗原因と再投入の新情報を照合する。wrapper内自動反復と親の再投入を同じ意味境界で判断し、必要なら先に修正・再配置のnext actionを示す。

External Reviewで確認した同一責務の具体条件（finalization-undeclared-form-routing）:

- PrepareFinalValidationWorkspaceの準備段階とfinalizationRoutingDecisionを接続し、requested formがない場合はvalidation/checkoutを起動せず既定reasonを返す。
- 破損candidate/別repository/未知formをno obligationへ握り潰さず、実controller StateBaseを使う正規入口を検証する。

## Must not

変更した候補に必要なfresh Go/race、source-bound evidence、独立Reviewを省略しない。失敗の一時waiverを恒久skipとして扱わない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

不変の失敗source/tool/timeoutなら無意味な二度目を実行せず、変更済みsource/tool/timeoutや不安定原因の有意な診断なら合法に再実行する。実行削減と誤skip防止、品質維持を実測する。既存PRで同ownerが修復済みなら重複変更しない。

- obligationなし、およびgo-testだけ宣言したcandidateへのgo-test-race要求がstatus:blocked/form_not_declared_by_accepted_candidateとなる。
- 正当な宣言formのcandidate専用checkout検証と、破損authority拒否を維持する。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
