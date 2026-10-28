# Task: Publication成功後のterminal handoff scope failureを正規完了経路で解消

## Original instruction

2026-10-29 ユーザー指示（原文）:

````text
https://github.com/shinderuman/codex-worker-orchestrator/issues/1424?utm_source=chatgpt.com

FindingはIMPLEMENTATION_PLANのPRを作るルールだろ
````

## Amendments

none

## Resolved references

- 「このIssue」: Web GPT監査で確認した、Publication自体は成功・remote公開済みなのに親向けterminal envelopeの後続handoff取得が失敗してコマンド全体がerrorで終了するFinding。実装要求の正本はこのTaskとcurrent Gitであり、Issueの管理状態・ラベル・番号をCodex/GLMのTask authorityへ持ち込まない。
- 実際の一次証拠: Dogfood Bundle `12ae3721-8dbe-4ac9-8d40-a8490d28f17c.zip`、`bound/transcripts/parent/01a18376-6c91-71f0-aefe-008d18809de9/0`、source `61d27c122f9b6460e8282d97ec7a3a6d2d9a6310`。Task `initial-glm-startup-stage-timing.md` の最終 `glm-parent-action controller-publication` が `complete, finalize-check:go-test, finalize-check:go-test-race, install-completion, promote, publish` を終えて `final_oid=remote_oid=61d27c...`、`next-task-not-started` を返した後、親向けwrapperは `parent_action_terminal_handoff_failed` / exit 1、error `parent evidence scope changed during projection; request fresh evidence`。親は `glm-worker --handoff recovery` とGit確認を追加実行し、二重Pushはしなかった。
- 関連する現行実装の調査入口: `glm-worker/internal/parentactioncmd/terminal_envelope.go`（実処理成功後に別command `--handoff` を起動し、失敗をwrapper errorへ変換）、`glm-worker/internal/parentevidence/support.go`（`CaptureReadScope` / `ValidateReadScope` がTask ID・status・lease epochを比較）。現行sourceでの原因確定前に、read-scope race・retire時状態変更・read-side副作用のいずれかに断定しない。
- 既存のterminal/handoff正常系とfail-closed境界は保護対象。親向け差分digestの同一性、serialized output budget、Publication stageの親往復削減、成功後の根拠なき再調査とは異なる。既存ownerにexact failure topologyが包含されると実証できた場合だけ同一ownerで重複実装を避ける。

## Purpose

Publicationの実処理が正常に完了した直後、同じ正規commandから返すべきterminalとpost-transition handoffの整合が崩れ、成功済み操作を失敗と誤認させて親Codexに不要な復旧探索を強制する問題を、一次証拠に基づき診断・修正する。不可逆のremote Publishを再実行させず、正規Task終端まで一貫したmachine resultを返す。

## External feasibility

status: not-applicable

## Contract

- 上記実Bundleの `publication success → terminal handoff read/parent evidence scope error → parent recovery` を、実行したsource・Task identity・Git/remote source・parent transcript・Controller/runtime statusと結び付け、どのread snapshot/lease/statusがいつ変わったのかを正規owner上で特定する。アーカイブ中の「後段でerror」という事実だけから外部競合・retire・scope guard自体が原因だと推定しない。
- 実Publication成功とterminal/handoff生成失敗を独立に扱う。必要なpost-transition handoffは既存canonical state authorityから取得し、正常Task完了で合法的に生じるstate/lease遷移と、真の同時更新・別Taskへの切替・矛盾したevidenceを区別する。scopeの有効性判定と正規fail-closedを保持したまま、通常完了経路で破綻しないようcanonical ownerで修正する。
- 不整合やhandoff取得失敗が実際に発生した際も、**Publish/Pushが成功済みか、remote OIDは何か、現在の合法な次actionは何か**を型付きの既存証拠で失わず、誤った再Publish・新Task起動・成功偽装・無制限な親側探索へ進ませない。既存のcontinuation/recoveryが適切なら再利用し、新しい第二state、第二handoff schema、再Push operatorを増やさない。
- 正常時は `controller-publication` の一度の親tool呼出から、正式なterminalとcanonical handoffが整合して返り、追加の独立 `--handoff recovery` / Git再照会を必要としない。既存の正規projection/lock機構を利用し、問題を隠すために常時広範なretry・再読込・親モデル再入を追加しない。
- 既存の関連Taskとcodeを比較し、完全に同一不変条件を既存ownerがすでに解決しているとsource-boundに証明した場合だけNo Changeを認める。過去のClose/テストPASSだけで問題解消とせず、今回のsource実行後にcontrolが実際に効いた証拠の有無を区別する。

## Must not

- 成功済みremote Pushを再試行して帳尻を合わせない。真のscope mismatchや権限不一致を無視・自動採用して成功にしない。
- parent evidence `ValidateReadScope` を無条件に無効化したり、すべての状態変化で常にallowしたりしない。正常terminalと矛盾するhandoffを、古いsnapshotから捏造しない。
- `parent_action_terminal_handoff_failed` のexit codeやerror文言だけを消し、正規handoffが生成されない実態を放置しない。成功と失敗が混在した状態を親が判別不能なままにしない。
- この問題を理由に全Publicationをもう一度実行させたり、CIや最終Go/race/smokeを重複実行したり、別の大規模System-One再設計を取り込んだりしない。

## Acceptance criteria

- 実Bundleの正規Publish成功後に `parent evidence scope changed during projection` が発生した具体的境界をcurrent sourceで特定し、cause / fix / independent effectsをsource-boundで記録する。再現不能なら、必要なbounded診断・未確定部分を明示し、推測修正でPASSとしない。
- 正規入口E2Eで、成功した単一Publication後に `completed` terminal・一致するfinal/remote OID・canonical post-transition handoff・`next-task-not-started` が、一回の親tool返却から矛盾なく観測される。正常完了がhandoff scopeの変化だけで偽FAILにならず、親が追加`--handoff recovery`を要求されないことを確認する。
- 別Taskへの切替・並行したlease/status変更・真正read-scope矛盾・handoffのI/O failureを別々に再現し、適切なfail-closed/typed recoveryを維持する。実Publicationの成功とwrapper failureを区別し、remote二重Push、未許可新Task進行、誤ったcomplete/adoptがない。
- 修正前後のparent model/tool往復、handoff再取得の有無、wall-clock、Codex token、および品質条件を比較可能な実証で示す。修正範囲のGo tests・正規Quality Gate・独立Reviewを通し、通常Completionと失敗・resume両方の回帰を確認する。

## Historical invariants

- source/Task/remote authority、immutable evidence、独立Review、最終Validationと一度だけのPublication、parent semantic decisionを維持する。
- 既存canonical handoff ownerと現在のread-scope safe guardを尊重し、旧schema互換、第二state DB、新daemon、新たなGuard回避経路を作らない。
- 最新Gitと本TaskのOriginal instruction/Amendmentsを要求正本とし、管理Issueは実装指示の第二正本にしない。

## Dependencies

none
