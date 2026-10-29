# Codex効率の中間評価（2026-10-29）

## 対象と比較限界

対象sourceはf129e9687cd3d1aabfd4ac9b0730a94534a304c2。2026-10-21の既存評価と10月29日までのGit、今回の親操作を評価した。Direct Codex対Codex+glm-workerの同一要求・source・model/reasoning・開始終了境界を持つ実測pairは今回確認した証拠にない。Codex ReductionとQuality Deltaはともに数値不明とする。GLM token、部分処理時間、cached inputをCodex利用枠の削減へ換算しない。

`glm-worker --stats history --since 2026-10-01T00:00:00Z --compact`は現在Taskのsession（5a1ce9d96722620535198497b8b622f86e82b55f4a600eaaca4e2af2fcc7dd84）を対象とし、telemetry files/archivesとも0件だった。これは全repositoryの10月使用量ゼロを意味しない。全sessionの再走査や追加A/Bは実施せず、既存評価を再利用する。粒度と比較条件の確定は既存codex-usage-comparability-evaluationへ集約する。

## 確認した効果と品質

正本は`review-evidence/workflow-performance-result.md`の限定計測・保証節。10月21日の同一roots archive captureは3回から1回、限定時間は305→235msと319→247ms。異roots例は477→491msで改善なし、unique bytesも減っていない。通常最終commitの重複lintは2回から1回。CLI fixtureの247.7秒と51.7秒は条件差があるため削減率を採用しない。同評価のworker16/reviewer2呼出、rate limit4件、packet8986 bytesにはparent usageがなく、Codex削減率は算出できない。

同評価には972.716秒、980.731秒の実suite FAILと別sourceへの修正が記録されている。fixtureのGuard欠落を復帰し、無効なGOTELEMETRY案が独立Reviewで撤回されたことは品質gateの具体的な効用である。最終current-source PASSやDirectとの品質同等性をこの履歴から推測しない。

Git 49774766の実差分は証拠走査・workspace blob読込の重複削減とその検証、01134aa3はETXTBSY防止、61d27c12は実行区間観測を追加している。機構の変更を確認したが、Task全体のCodex削減量は未測定。bd1873aeはA/B欠落usage拒否を既に実装しており、旧ReviewのF12を再起票しない。

今回の会話ではparent-only checkpointへの誤ったGLM開始、要求同期前のlane作成、binding欠落によるhandoff不整合、短い待機と失敗報告後の再開要求が親往復を増やした。モデル呼出前の拒否をGLM作業の成功・失敗件数へ数えない。親の指示適用ミスと、初期化前stateをhandoffが扱えない構造を区別する。実測token差は不明だが、不要な親往復の存在はこの会話と実tool結果で確認済み。既存task-revision-worker-admissionとparent-inflight-wait-admissionへ統合し、新しい復旧frameworkは作らない。

## internal/appの実態と原因

同sourceの直下Goファイルは216、production 74ファイル/9,242行、test 142ファイル/28,898行、合計38,140行。内部package importは30種類。行数は空行・コメント込みで、複雑度や削除可能量ではない。

- `dispatch.go:22`以降の5分類はCLIのread-only/control/mutation分離として保持する。composition rootのimport数だけを欠陥としない。
- `telemetry_summary.go:132`以降はCLI出力だけでなく履歴走査、schema cohort選択、outlier集計、親usageの合成を所有し、関連ファイルに25関数がある。既にbundleanalysis/report/stateが存在するのにappが分析機能ownerとして残り、新指標追加がappとそのテストへ流入する。集計の意味をreport/bundleanalysis側へ閉じ、appはquery入力・出力に限定する具体Taskを新設する。
- `controller_task_authority_sync.go:18`以降はruntime取得、同期前証拠の採取、controller更新、runtime同期を跨ぐ。`controller_execution.go:196`以降の復旧もruntimeに依存し、`parentcontinuation/projection.go:418`以降はbinding欠落を返す。今回、要求syncは成功してもbindingは生成されず、recover-runtime-startはbinding生成後に未開始runtimeを拒否した。操作のerrorと実副作用を同一ownerで閉じる必要がある。まず既存task-revision-worker-admissionで具体的不整合を修正し、別の構造Taskでappから遷移の順序・失敗回収を移管する。ファイル移動だけでは達成としない。
- `controller_task_authority_sync_test.go:57`のfixtureはcandidate reentry、materialize、state baseline・binding・要求の手動準備を行う一方、同file:430以降はfresh startのguardを検査する。後者はhandoffまで確認せず、今回のbinding欠落を捕捉していない。layer単体テストのPASSと通常経路の閉包を分ける。
- `startup_cost_test.go:17`と`parent_handoff_test.go:45`等は実controller/CLI境界の意味があり、一括削除しない。test行数76%はそれだけで冗長性の証拠ではない。ownerの単体検証をowner packageへ、appにはparse/dispatch/JSON・正規失敗復旧の接続検証を残し、同じ長いfixtureの反復を減らす。既存self-referential-test系列が扱う自己確認型testを新Taskへ複製しない。

確認範囲では、新たに断定できる死コードや同一アルゴリズムの重複は特定していない。旧experimental経路はsystem-one、通信adapterはcommunication-transport-unification、自己確認型テストは既存3Taskへ集約する。「テストがあるからKEEP」も「行数が多いからREMOVE」も採用しない。全source再監査は行わない。

## 優先判断

全45既存TaskのPurposeとPlan位置、重要なContractを照合した。優先は(a)現在のraceと要求/復旧/Publicationの整合、(b)親往復抑制と検証再投入、(c)使用量比較の確立とapp責務集約、(d)残る証拠・data保護、(e)広い機能整理と有機的E2E、(f)探索/rotation等の条件付き最適化とする。Planの順番を正本とし、本書へ全scheduleを複製しない。BLOCKEDの許可条件は解除しない。

app集計ownerと遷移ownerの2件を具体化し、既存Taskと責務を分ける。次回checkpointは先頭5件の実装完了後、重大incident時は前倒し。post-105評価は022直前を維持する。現在の評価は計画化までであり、これらのリファクタリングを開始しない。

## 親専用作業のコミット・完了阻害の起票

2026-10-29の追加確認で、通常commitはpre-commit→guard内のgit ls-files失敗、primary index絶対pathを指定したcommitはlane snapshot不一致で拒否された。laneで相対GIT_INDEX_FILEが`.git`ファイル配下を指して失敗することを直接確認した。guard単独の成功をcommit成功の証拠にしない。

また、親専用checkpointに通常Runtime/独立GLM Reviewerを求める完了経路の不整合は、全Codex単独作業が不可能という一般的な結論とは区別する。ユーザー指示に従い、この評価で実装を開始せず、`git-hook-worktree-environment-isolation.md`と`parent-only-task-completion.md`を独立起票した。前者はGit入力の隔離、後者は親専用Taskの要求・成果・検証・採否に基づく正規終端を扱う。PlanのNEXT先頭へ配置し、canonical-publication-source-continuityの既存追記は独立Task参照へ整理した。これらは現在の終了を阻むFindingであり、起票を修正済み・コミット済み・Task終了済みの証拠にしない。

## 親採否と今回限りの終了境界

親Codexは、比較不能値をunknownに保った効率・品質評価、internal/appの責務評価、既存Taskとの重複整理、具体的Task化、全scheduleの整合、次回checkpointと022直前のpost-105維持を確認し、本評価の成果を採用する。production code/test/configは変更せず、追加モデル呼出と新しいA/Bも行っていない。検証は実差分とsourceの照合、Plan/Task参照の閉包・重複確認、git diff --checkであり、runtime変更に対するGo PASSを主張しない。

ユーザーのstate直接変更許可に基づき、誤って作成した未開始GLM attemptをuser-terminatedとし、Controllerのlive attempt/lease/execution参照と未開始runtime bindingを外した。元stateは`/tmp/codex-checkpoint-state-repair-20261029/`へ退避した。親評価Taskの採用と、不要なGLM実行attemptの終了を区別する。この例外修復を正規parent-only完了機能の実装済み・検証済み証拠にせず、起票した2件は未実装のまま保持する。通常GLM Reviewer/PASSや必要validationの偽造は行わない。
