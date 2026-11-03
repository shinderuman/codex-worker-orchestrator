# 巻き取り完了後のCodex効率・テスト時間評価

## 比較境界と使用量の限界

前回評価は`review-evidence/codex-efficiency-webgpt-adoption-2026-11-03.md`、Git基点は`458145010b5e112c8ddb1474f69ae730c22bb9d7`、今回終端は`701eae45adedc0661f890ae3029402f3eef2dd1d`。追加対象は#1485のsmoke process回収と#1483の遷移調停移管であり、完了済み先行5候補を再監査しない。本評価は親Codexだけで行い、GLM・他モデル呼出、新しい本番A/B、production変更を行っていない。

この区間と対応する親input/output/cache/reasoning使用量、および同一要求・source・model/reasoning・開始終了境界のDirect Codex対Codex+GLM pairは取得できていない。Codex Reduction率と比較Quality Deltaはunknown。以下の実行秒数、fixture数、コード行数をtoken削減率に換算しない。比較可能性の確立は既存`codex-usage-comparability-evaluation`へ維持する。

## 時間としてのinternal/app肥大化

ユーザーの「Appの肥大化というのは主にテストの時間に対して言っている」を評価軸とする。最新の既存full Go/race記録は次のrunで、両方ともhead `314bf409c1a4d1760055acddc9764b94801dedc3`、index digest `b4e60f6fefb4f63ef2854989918100ce1998a7029df1ab447e6b4d0d15266602`、worktree digest `8f1b4ee756d23341b3c7fa2cf4f68394c521c1ae76d690b0961203c2cd013dab`へ束縛されている。

- 通常: `f252d16b5d1ce2fbaaefccc738b38666`、gate全体295.067秒。
- race: `e8693d9a3c1c17b1e31fc51bca1587ab`、gate全体738.328秒。

一次記録は`/Users/shinderumanm/.glm-worker/sessions/c923e74baf597dd24e2f45bf69e777b9cf03e46b4e1ab47168b98a28aee58b53/quality-gate-runs/<run-id>/run.json`と`gate.log`。下表はgate.logのpackage terminal pass eventのElapsedであり、test別elapsedの合算ではない。packageは並行実行されるため、縦方向へ合計してgate時間としない。

| package | 通常 秒 | race 秒 | 通常 / race log行 |
| --- | ---: | ---: | --- |
| app | 287.676 | 722.579 | 30221 / 30221 |
| controller | 131.052 | 176.855 | 26210 / 25448 |
| workflow | 45.761 | 59.729 | 23225 / 22569 |
| runner | 43.853 | 48.866 | 21674 / 19711 |
| state | 16.679 | 56.806 | 14457 / 21451 |
| parentactioncmd | 21.523 | 21.153 | 15368 / 12621 |

appが通常・raceとも終了時刻の主な制約である。appの遅い上位3testは以下。いずれも全体testのelapsedで、子subtestと二重加算しない。

| test | 通常 秒 | race 秒 | 両logのterminal行 |
| --- | ---: | ---: | ---: |
| TestTypedParentCompletionWorkflowThroughProductionBoundary | 23.44 | 74.06 | 27277 |
| TestParentOnlyTaskCompletionThroughProductionBoundary | 19.03 | 72.45 | 27317 |
| TestParentOnlyReentryThroughProductionBoundary | 17.98 | 80.34 | 27329 |

`typed_parent_completion_e2e_test.go:51`は4scenario、`typed_parent_only_completion_test.go:29`は3scenario、同`:48`は2routeで、それぞれfixtureを作り実際の完了・再入経路を駆動する。前者`:65`と後者`:206`のworker shimは`os.Executable`のtest binaryをsubprocessとして再起動し、`runTypedPublicationCLI`（前者`:239`）はprepareと実actionを実行する。parent-only fixture（後者`:246`）はGit corpus・remote、Controller activate、candidate accept/promote/publishまで準備する。繰り返されるsetupとprocess境界は改善の調査対象になるが、Git処理、race付きprocessの起動・終了、snapshot/証拠処理の時間内訳と起動回数は未計測であり、支配原因を断定しない。通常完了、失敗後の再入、異なる実行指定を保証する意味の違いがあるため、遅いという理由でscenarioを削らない。

比較用の前回#1481記録は`/private/tmp/issue1478-pr1481-validation-go-test.json`と同`-race.json`が指すrun `01ef09bdc03a607c2f031b578c4a6f2c` / `67c49fd932b352d8247aa97d875cab90`。appは295.454 / 702.175秒、controllerは141.882 / 180.840秒、workflowは38.549 / 46.607秒、stateは9.448 / 44.679秒だった。今回のapp通常は短くraceは長い。sourceも実行時負荷も揃っていない単回観測なので、#1483による改善・回帰の因果や削減率は主張しない。少なくとも今回の責務移管だけでappの長いtest時間が解消したという証拠はない。

他packageも確認した。controllerではroot promotion復旧が通常27.71秒（log:25779）、別のnested recoveryが17.03秒（25001）、raceでは32.68秒（25375）と20.13秒（24091）。stateのreplay fence capacity/recoveryはrace36.22秒（15389）。runnerのdeadline/process-group系は通常各約12秒であり、待機時間自体に検証上の意味がある可能性を残す。いずれもappと同じ原因だとは判断せず、全package一括のtest削除・並列化や新しいcacheを計画しない。

## CI平準化

追加ユーザー要求により、[指定CI run 41600207557](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41600207557)を確認した。GitHub Actions jobs APIのstarted_at/completed_atを差し引いた以下の時間はすべてhead `701eae45adedc0661f890ae3029402f3eef2dd1d`へ帰属する。コネクタのジョブ一覧では時刻が省略されるため、同APIの時刻fieldだけを別途取得した。今回のローカルrace runとは条件が異なり、値を混ぜない。

| job | 全体 秒 | 主処理 秒 | job ID |
| --- | ---: | ---: | ---: |
| basic (quality) | 180 | lint 140、vet 19、build 2 | 125796886199 |
| basic (controller) | 164 | controller test 146 | 125796886507 |
| basic (test-1) | 51 | test 32 | 125796886527 |
| basic (test-2) | 236 | test 218 | 125796886618 |
| basic (app) | 438 | app test 419 | 125796886767 |

上記5jobと集約lintはsuccess、install-smokeはskippedだった。appが最長であり、他packageのtest-1とtest-2にも32秒対218秒の偏りがある。`.github/workflows/ci.yml:73`以降はapp/controllerを除くpackageを名前でsortし、package数の半分で分割する。`:91`以降はapp/controllerを専用jobとして実行する。package数による均等分割は時間の均等分割ではない。runner待ち・cache・コンパイル・個別testの寄与はこのjob要約だけでは分離できず、ここで分割案の最終採用はしない。

`app-test-fixture-runtime-cost`へCI平準化とcritical path短縮を追加した。app内の重いtest改善に加え、既存CIの配分を時間根拠で評価する。全test/race/必要な実境界coverage、集約gateの失敗伝播とsource帰属を保ち、分割によるsetup重複・総実行量増加も比較する。

## importer Panicとparent-only Bundle失敗

今回の最終確認でPATH上の`harnesslint`は`panic: cannot decode "crypto/sha256", export data version 4 is greater than maximum supported version 2`となった。stackの該当境界は`go/internal/gcimporter.Import`から`harnesslint.qualityWiringPartialBindings`。観測した実行Goは1.27.0 darwin/arm64、GOROOTは`/opt/homebrew/opt/go/libexec`、配置済み`/Users/shinderumanm/.local/bin/harnesslint`のbuild Goは1.25.4だった。toolchain不一致との因果は未確定で、前回のimporter Panicと同一原因とも断定しない。

repositoryの既存wrapper `./harnesslint`はquality-tools.ymlのGoを選択し、同じ評価変更でexit 0、pass/fixed 0/violations 0となった。記録は`/private/tmp/codex-efficiency-followup-2026-11-03-lint.json`。これは今回のLint成立の証拠であり、PATH側のPanic修正ではない。

ユーザーからparent-onlyではBundleを生成できないとの報告、および同じPanicが原因なら同Taskで修正する要求を受けた。今回の評価ではそのBundle失敗を再現しておらず、共通原因はunknown。前回にもimporter Panicがあり、定型的な入口選択を親がやり直す往復と品質処理の停止を再発させるため、`harnesslint-importer-parent-only-bundle-failure`を独立Taskとして採用した。配置済み入口・production quality・parent-only Bundleの実経路を調べ、共通原因なら一体で修正し、通常Taskとparent-onlyの正規Bundle生成・完了接続を検証する。wrapper成功や手動Git同期をBundle成功の証拠にしない。

## 実差分・責務・品質

通常Goとraceのrun.jsonはいずれもpass、gate.logにはfail eventがなく、各74 packageのstart eventと36 skip eventを確認した。成功を未実行項目の成功へ拡張しない。検証時module tree `12ae1fd8d52a0aa6a121bbad83df02278b8ae890`は今回終端の`HEAD:glm-worker`と一致する。source対応は`/private/tmp/issue1478-pr1483-validation-source.json`を併用し、終端MetadataのHEADが検証時HEADと同じだとは表現しない。

#1483の実差分ではappのexecution dispatchからworkflowへ一度委譲し、同期前証拠・Controller更新・Runtime completion・typed recoveryをworkflowが所有する。`controller_execution_transition.go`と`controller_reentry_review_transition.go`で同じtransition IDによる復旧、retained reviewのTask/attempt/実source照合、controller-only時のRuntime不要を確認した。`controller/admission.go`の共有admissionはconfig/Storeのrepository lineageとStateBaseを照合する。app内の旧遷移実装の削除と移管先を照合した。

CLI接続test `app/controller_reentry_workflow_test.go:56`以降は実Controller commit後に未完Runtimeを残し、typed recoverでbinding/checkpoint/handoffを確認する。同じtransitionの再実行と異attemptの拒否もassertする。Draftの重複test1件を既存fixtureへ統合した記録は`/private/tmp/issue1478-pr1483-fixture-projection.json`。これはDraft比1 fixture起動撤去の証拠であり、main比の全fixture削減やapp時間の改善を証明しない。

前回未達のsmokeは`/private/tmp/issue1478-pr1483-pre-retire-smoke-proof.json`と同log:119のpassを確認した。通常TaskがACTIVEだった終了Metadata前のsourceに帰属し、module treeは上記と一致、script blob `82f648c41a2dd6e5026ec7654dbd632726372e96`は終端`tests/install_smoke.sh`と一致する。#1485の変更後scriptも`/private/tmp/issue1478-pr1485-final-smoke-proof.json`と同log:119でexit 0・passを確認した。parent-only ACTIVEでも通常fixtureが独立することの証明にはならず、既存isolation Taskは維持する。

PR #1483/#1485の既存discussionには親Codexの条件付き採否とCodeRabbitのreview skipがあり、最終sourceへの独立Reviewer PASSの証拠としては使えない。本checkpointは親専用評価なので追加review/model callを行わず、この制約と実source検証を区別して記録する。

規模は補助情報として、基点からapp productionが9,438→9,056行、testが31,400→31,463行。controllerは32,667→32,725行、workflowは12,464→12,934行、stateは15,385行のまま。空行込みのGo行数であり、appからの382行減少とworkflowの470行増加をtest時間の削減とは扱わない。controllerのevidence_export系5,363行、appのquality_gate系1,353行等も集中候補だが、時間内訳なしに別の大規模構造改修を開始しない。

## 改善の採否と優先順位

新しい`app-test-fixture-runtime-cost`を計画化する。最大のapp完了・再入test群と既存CIの時間配分に絞り、fixture setup、Git、subprocess、race追加時間の内訳を測定し、意味の重複があるsetupや実境界反復を減らす。ownerの単体検証と少数のCLI接続検証への分離はcoverage対応表と親の採否を先に必要とする。test impactによるrun省略、scenario retirement、race検出の弱化は含めず、BLOCKEDの`104-test-impact-selection`を解除しない。遅い他packageについては数値を残し、支配原因未特定のまま独立した最適化Taskを増やさない。

全28既存TaskのPurposeと重要なContract/Dependencies、Plan位置を照合した。transport統一、System-One整理、usage比較、publication往復評価、guard評価、通常workflowの有機的E2Eは別責務として維持する。今回の測定・fixtureコスト改善は既存のsmoke authority修正、quality記録の正確性、test selection/retirementと区別する。未実装のadmission/terminal handoff/無変化再入の問題を完了扱いせず、同種Findingを重複起票しない。BLOCKEDの許可条件を変更しない。

実行priorityはPlanだけを正とし、本評価を正規完了できた後にsmoke fixture独立化を次ACTIVE、Panic/parent-only Bundle修正とapp test時間・CI平準化を早期へ置く。次回中間評価は本評価後の最大5件の完了後に配置し、post-105を022直前に維持する。親Codexは上記のunknown、source対応品質証拠、test時間のボトルネックとbounded改善Taskを採用する。NEXTの実装は開始しない。追加要求のTask化は不具合修正の完了を意味しない。

## parent-only実行経路と今回の手順誤り

最初の評価では親CodexがGLMを呼ばずに直接作業した一方、Controllerのparent-only attemptを開始しないまま手動commit/pushへ進んだ。Taskにmodeを記載することと、正規parent-only実行は同じではない。publication guardの「controller authority is unavailable」は、この親orchestrationの手順誤りによる未登録状態の証拠であり、ユーザー報告のparent-only Bundle不具合の再現証拠に使わない。

未公開commitはlocal branchとstashへ退避し、公開済みsourceを基点に同じACTIVEのparent-only-executionをController ownerでmaterializeした。通常Runtimeのbootstrap拒否をparent-only開始成功とは扱わない。今回の復旧ではTask定義とsource snapshotの不一致も検出したため、退避済みの未公開Task編集を戻す実変更を既存AdmitMutation/RecordAdmittedMutation APIで記録し、同じattemptを保持してconsistentなparent-only完了操作へ復帰した。State fieldの直接編集、Guard無効化、GLM呼出は使っていない。

次回Codex評価では、親が直接作業したという説明だけでparent-only成功とせず、実行指定、Controllerに登録されたexecution authority/attempt、Taskとsourceへのbinding、親acceptance証拠、正規Bundle生成・Publication/Retireの成立を評価する。Panic、親自身の手順誤り、product側のBundle生成失敗を別々の一次証拠へ帰属させる。今回の未登録状態を製品バグの再現として誤用したり、登録不足で復旧可能な作業を停止したこと自体も再発防止の評価対象とする。この確認事項は評価本文に保持し、ユーザー指定どおり次回TaskやAmendmentsには追記しない。

正規BundleやPublicationは実際に成立した証拠で判断し、この本文の意味採用や手動Git処理を代用しない。現在taskの完了後はNEXTを開始しない。
