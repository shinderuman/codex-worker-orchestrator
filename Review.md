# 総合レビュー：12観点の横断確認

2026-09-23再開分までのレビュー記録: **具体的不具合16件**。下記初回集計の後に追加Findingを追記しており、累積件数の途中記述はその時点の値である。最新の全件計画化要求に従い、16件を各々独立Taskへ対応付けた。今後の実装契約は各Task、priorityは `IMPLEMENTATION_PLAN.local.md` を正とし、このfileは根拠・再現・調査限界を保持する。原実装は修正していない。

再現コード12fileと失敗診断18件を `review-evidence/` へ保存した。一時directoryが消えても再現手順・assertion・根拠を引き継げる。実行方法は `review-evidence/README.md` を参照。レビュー中はGLM・他モデルへのレビュー委譲を行っていない。計画化後のmainへのcommit/pushは後続のユーザー要求による。

## Findingと実装・評価Taskの対応

以下はレビューIDから要求正本への参照であり、実行順序ではない。すべての参照先をPlanのscheduleに含める。後方互換性禁止・適用owner・受入条件は各Taskに固定する。

| Finding / 改善候補 | 要求正本 |
|---|---|
| F1 Codex設定の同時編集消失 | [codex-install-concurrent-edit-protection](IMPLEMENTATION_TASKS/codex-install-concurrent-edit-protection.md) |
| F2 fixer後のreview snapshot不一致 | [quality-fixer-review-snapshot-boundary](IMPLEMENTATION_TASKS/quality-fixer-review-snapshot-boundary.md) |
| F3 Codex installer中断復旧 | [codex-install-interruption-recovery](IMPLEMENTATION_TASKS/codex-install-interruption-recovery.md) |
| F4 review済みledgerの非現行schema受理 | [reviewed-ledger-current-schema](IMPLEMENTATION_TASKS/reviewed-ledger-current-schema.md) |
| F5 復旧probeの停止無視 | [recovery-probe-stop-boundary](IMPLEMENTATION_TASKS/recovery-probe-stop-boundary.md) |
| F6 CLI installer中断復旧 | [cli-install-interruption-recovery](IMPLEMENTATION_TASKS/cli-install-interruption-recovery.md) |
| F7 owned binaryのmode検証不一致 | [cli-install-owned-mode-verification](IMPLEMENTATION_TASKS/cli-install-owned-mode-verification.md) |
| F8 Claude設定の同時編集消失 | [claude-settings-concurrent-edit-protection](IMPLEMENTATION_TASKS/claude-settings-concurrent-edit-protection.md) |
| F9 異なるlocatorの本文dedup混同 | [parent-evidence-locator-identity](IMPLEMENTATION_TASKS/parent-evidence-locator-identity.md) |
| F10 分割evidenceのcoverage欠落 | [parent-review-evidence-coverage-accumulation](IMPLEMENTATION_TASKS/parent-review-evidence-coverage-accumulation.md) |
| F11 新規symbol・削除行の証明不能 | [parent-review-target-evidence-coverage](IMPLEMENTATION_TASKS/parent-review-target-evidence-coverage.md) |
| F12 欠落usageの100%削減誤計上 | [ab-eval-usage-field-presence](IMPLEMENTATION_TASKS/ab-eval-usage-field-presence.md) |
| F13 品質tool暗黙設定の保護漏れ | [quality-surface-implicit-tool-config](IMPLEMENTATION_TASKS/quality-surface-implicit-tool-config.md) |
| F14 別WorkingDirの検証共有 | [quality-gate-working-directory-identity](IMPLEMENTATION_TASKS/quality-gate-working-directory-identity.md) |
| F15 wake規則の無関係automationへの適用 | [codex-wake-inventory-ownership](IMPLEMENTATION_TASKS/codex-wake-inventory-ownership.md) |
| F16 既存untracked変更・削除の差分欠落 | [task-diff-preexisting-untracked-baseline](IMPLEMENTATION_TASKS/task-diff-preexisting-untracked-baseline.md) |
| quality二重検査と呼出形状を固定するguard | [quality-gate-single-validation-pass](IMPLEMENTATION_TASKS/quality-gate-single-validation-pass.md) |
| 022固有policyの責務分離 | [final-verification-policy-ownership](IMPLEMENTATION_TASKS/final-verification-policy-ownership.md) |
| publication定型stageの親往復削減 | [publication-sequence-roundtrip-evaluation](IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md) |
| rotationの再読・cache費用 | [parent-session-rotation-cost-evaluation](IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md) |
| A/Bとparent usageの比較粒度 | [codex-usage-comparability-evaluation](IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md) |

review回数削減・impact test選択・compaction thresholdのproduction変更は既存の106/104/103のBLOCKED条件を維持する。System-Oneは既存ACTIVEのshadow契約を維持する。任意secret用の新汎用検出器と全state DB統合は不採用とし、実装Taskを追加しない。これらの採否は中間checkpointのContractにも固定する。

2026-09-22。GLM/model callなし。対象sourceは `dc6f182`。再開時に直前のレビュー記録commit `9ffd07e`以降の差分を確認し、変更されたrepo-searchのread-scope/dedup testも追加確認した。source修正は行わず、採用findingをparent-managed Task/Planへ登録した。

## 確認済み不具合

| 優先度 | 問題と発生条件 | 影響・根拠 |
|---|---|---|
| P1 | installer事前確認後に管理外config keyが編集される | prepare時に保存した全体をapplyが置換し、成功したまま編集を消す。[config.go](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/codexinstall/config.go:106)。今回の一時testで再現 |
| P1 | 正規quality fixerがfileを変更する | worker-end snapshotはfix前、review-startはfix後のため、正常な変換でreviewerを呼ばず親review待ちへ停止する。[review_flow.go](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/workflow/review_flow.go:21)。前回のproduction workflow fixtureで再現 |
| P2 | installerがfile/config更新後、ownership state保存前に終了する | メモリ上のrollback情報が失われ、再実行が自身の更新をユーザー変更として拒否する。[transaction.go](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/codexinstall/transaction.go:31)。今回の子process終了testで再現 |
| P2 | reviewed blob ledgerへ非現行versionを渡す | version 0/999を既review証拠へ取り込める。[reviewer_boundary.go](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/workflow/reviewer_boundary.go:49)。前回の一時testで両readerを確認。実際のmodelがreviewを省略したという観測ではない |

installerの再現は一時repository・一時配置先だけで実施した。process終了はstate writer直前のos.Exitであり、OS電源断・fsync耐性まで証明したものではない。競合testはprepare/apply間の決定的なinterleavingであり、実editorとの確率的race testではない。

## 12観点の結果

| 観点 | 確認したowner・証拠 | 結論・限界 |
|---|---|---|
| 1. 正常taskの親介入 | [publication sequence](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/publicationsequence/sequence.go:51)、parentactioncmd/publication*、23 test/subtest成功 | machineは次actionをprojectionできるが、実行sequenceそのものではない。意味判断が不要な隣接stageの親tool往復をまとめる候補。tool call数をそのままmodel turn数とは数えない。remote writeと意味判断は親に残す |
| 2. 機械化の費用対効果 | [quality gate](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/workflow/quality_gate.go:31)と[harnesslint runner](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/harnesslint/run.go:57)、control-provenance | Run(fix=true)内の検査の後にCheckで同じ検査を再実行。重複は確定。wall-clockとtoken効果は未測定。新guard追加よりこの重複解消を優先 |
| 3. 状態の正本・重複 | state/task_state_lifetime.go、parent_complete.go、parent_action.go、state対象66 test/subtest成功 | task-bound stateの寿命表とcanonical completionがある。statsをcompletion authorityにしないnegative testもある。単にfile数が多いという理由で集約DBを追加しない |
| 4. 境界間の整合 | review_flow→quality gate→snapshot、codexinstall files/config→ownership state | 個別機能の正しさでは足りず、合成時に2種類の停止不具合を再現。追加すべきtestは実際の変換・process終了を含む境界test |
| 5. 中断・再試行・並行実行 | state/parent_action_begin.go、parent_reopen_transaction.go、settingsmerge/transaction.go、cliinstall/serialization.go、codexinstall/transaction.go | 親action/settings mergeにはdurable記録、CLI installerにはlockがある。Codex installerは同等の復旧・競合保護がなく、新規2件として登録。全state write地点のcrash matrixを網羅したわけではない |
| 6. 証拠の有効範囲 | state/parent_review_evidence.go、app/parent_evidence_support.go、app/repo_search.go | task/lease/review ID/snapshotへのbinding、古いscopeと重複配信の拒否を確認。tracked要求fileもworktree snapshotに含む。reviewed ledgerのversion欠落は別の穴として登録。構造的proofと意味的な読解を混同しない |
| 7. 原要求とderived contract | workflow/active_task_prompt_contract.go、activetask_test.go、metadata guard | worker/reviewer双方への原要求・amendment参照とreviewer独立比較をprompt/testで確認。promptが入ることは実modelの理解証明ではない。conversation ingressの完全性は既存BLOCKED taskの範囲を維持 |
| 8. テストの証明力 | 対象既存test群と今回のoverlay再現 | 既存testが成功しても更新中断・事前確認後の編集消失は漏れる。quality-wiringのstrings.Containsは呼出形状に依存し、意味同等のrefactorにも反応する。Goの順序検査にはASTも使うため「全て文字列検査」とはしない |
| 9. source/runtime整合 | parentactioncmd/runtime_install.go、runtime_install_config.go、codexinstall/Verify、cliinstall/Verify | revision/source digestだけでなくCodex・Claude設定とCLI所有権の検証がある。対象testでmetadata-onlyのHEAD前進許可とruntime変更拒否を確認。本番install・installed smokeは今回未実行。install.sh全体の複数installer跨ぎatomicityは保証未確認 |
| 10. 権限・data境界 | runner/git_authority_sandbox.go、git_authority_proxy.go、sensitive_artifact_admission.go、51 test/subtest成功 | protected Git metadataとnetwork制限、sandbox不可時の拒否、既知secretのartifact検出を確認。provider本番での実効性、未知secret・変換されたsecret・全出力経路までの非漏洩は未証明。漏洩が発生したと断定しない |
| 11. ruleの重複・廃止 | taskcontract/final_verification.go、harnesslint/schedule_closure.go、quality_surface.go、Markdown authority tests | generic parserが022固有policyを持つためharness ownerへ移す。親instructionには意味判断を、決定可能な手順にはmachine ownerを維持。前回記録以後のtracked Markdown差分はPlanと3 taskのみ。全Markdown再読や意味的一致を判定する新LLMは追加しない |
| 12. 実消費の帰属 | app/bundle_parent_usage*.go、abeval/abeval.go、usage/evidence対象71 test/subtest成功 | task execution/finalization・cache/reasoning・counter reset・混在turnを区別し、曖昧な使用量をknownにしない。一方A/B CodexUsageはinput/output中心で粒度差がある。実task cohortがないため削減率・rotation ROI・quota消費との換算はunknown |

## 責務分離の判断

- generic worker：model call、packet、session、task identity、一般的なsnapshot/証拠のlifecycle。
- repository harness：tracked markerで有効化したPlan/Task保護、quality policy、最終検証taskの具体的な順序。
- 親Codex：要求の意味、architecture、Go/No-Go、risk、最終採否、remote write。
- repository instruction：ownerと意味contractを示す。machineが返すschema・command順序・thresholdの第二正本を増やさない。

[022 policy](/Users/shinderumanm/src/codex-worker-orchestrator/glm-worker/internal/taskcontract/final_verification.go:3)は上記の境界を越える具体例。ただしconsumerはharnesslintであり、無関係な全repositoryへ無条件適用される障害とまでは確認していない。markerなし／不正marker／moduleだけ存在／foreign Planの境界testを確認した。

逆方向では、evidenceのdedup/leaseやpublicationの現物検証を親の手動再計算へ戻すべきではない。現行instructionはcontrol ownerを参照しており、その整理を維持する。reviewや原要求の意味比較はmachineへ移してよい決定的ruleではない。

## 採否・優先順位

今回のGoは「独立修正taskとして採用」であり、実装済みという意味ではない。

| 候補 | 採否 | 根拠 |
|---|---|---|
| installerのユーザー編集保護 | Go・NEXT最優先 | data消失を再現。安全停止または変更保存を要求 |
| quality fixer/snapshot整合 | 既存Go継続 | 正常処理から不要な親復帰を再現 |
| installerの中断復旧 | Go | 正規再実行不能を再現。current transactionの復旧であり旧schema互換ではない |
| reviewed ledger current schema | 既存Go継続 | 非現行version受理を再現。migration/推定promotionなし |
| quality gateの二重検査解消 | Go | 同一呼出内の二重検査をsource確認。coverage保持で整理可能 |
| 022固有policyのowner移動 | Go | 単独consumerと固有pathが明確。新framework不要 |
| publication定型stageの連続実行 | 現時点では保留 | 既存typed projectionを利用できるが、実際の親model往復の主要因というcohort証拠なし。巨大finalize APIを先に追加しない |
| quality-wiringの実装形状固定を削減 | 対象変更に付随して判断 | 重複gate解消taskで旧Run→Check強制を置換。全guardの一括削除は不採用 |
| rotation threshold変更 | 保留 | pendingはrecommendationでdecline可能。claim後のtransaction guardとは別。rotation前後の比較可能なtoken証拠なし |
| A/Bとparent usageの測定粒度統一 | 既存効率checkpointで継続評価 | cache/reasoning/unknown/source locatorを落とさず比較する必要がある。比較cohortなしに新schema/bridgeを先行追加しない |
| review回数削減・impact test選択 | 既存BLOCKEDを維持 | quality非劣化の証拠不足。今回の合成不具合は検査省略を支持しない |
| System-One evidence filter | 現ACTIVEのshadow契約を維持 | full canonical auditが残る段階は即時token削減ではない。自動production採用しない |
| 任意secretの新汎用検出器・全state DB統合 | 不採用 | 今回確認した不具合の直接修復ではなく、保守と誤検出の費用対効果が不明 |

Planでは既存ACTIVEとBLOCKEDを維持し、NEXTをユーザーdata保護→正常review停止→中断復旧→schema→重複検査→責務移動→効率再評価→最終再評価→022の順へ整理した。順序をhard dependencyに読み替えていない。今回の静的レビューでは既存中間checkpoint全体の実消費acceptanceを満たせないため、そのtaskを完了・削除していない。

## テストと証拠

| suite | top-level PASS | subtest込みPASS |
|---|---:|---:|
| state | 37 | 66 |
| install | 50 | 60 |
| containment | 28 | 51 |
| publication | 17 | 23 |
| usage-evidence | 46 | 71 |
| requirements-runtime | 22 | 50 |
| scope-rules | 33 | 56 |
| 合計 | 233 | 377 |

新規installer再現testは2件とも期待する安全動作を満たさずFAILした。既存test群にはFAIL/SKIPなし。前回のsnapshot/schema再現testはこの377件には含めない。

- [実行条件・対象regex](/private/tmp/codex-repository-review-extended/run_checks.py)
- [集計](/private/tmp/codex-repository-review-extended/test-summary.json)
- [再現コード](/private/tmp/codex-repository-review-extended/install_audit_test.go)
- [再現log](/private/tmp/codex-repository-review-extended/install-reproduction.jsonl)：失敗diagnosticは4行目と9行目。
- [前回詳細・残る2不具合](/private/tmp/codex-repository-review/review.md)
- suite別JSONLは上記集計のsource locatorを参照。

Go 1.25.4、canonical cache、GOPROXY=offを使用した。実model/API call、全Go suite、race detector、live provider検証、本番install、実Codex A/Bは未実行。12観点を横断して調査した結果であり、全source行・全crash地点・全security経路を網羅した保証ではない。raw token量から利用枠の消費率を推定していない。

## 追加レビューと計画化の確定

ユーザーの最新指示に従い、成果物をmain上のレビューと既存Plan/Taskへの計画化に限定する。Git作業とGuard通過は完了条件にしない。

- 追加確認：repository全体のharnesslint・go vet・go buildが成功。protocol解析、process停止、復旧loop、lock、installerの依存/tool取得経路も確認した。これらは本番外部サービスの成立性の証明ではない。
- 新規P1 finding：runner/probe.goは通常Runの停止処理を使わず同期command.Runを呼ぶ。停止要求済みcontrollerでも子processを起動して成功することをfake CLIで再現した。復旧probe中の停止とdeadlineの責務を `IMPLEMENTATION_TASKS/recovery-probe-stop-boundary.md` へ固定した。
- 再現証拠：/private/tmp/codex-repository-review-extended/probe_audit_test.go、probe-reproduction.jsonl:4。診断は `stop ignored: error=<nil> child_started=true`。実model呼出しなし。
- 計画化：不具合5件と改善2件を独立7 taskとし、Plan NEXTへ優先順で配置した。実消費の比較が必要なpublication連続処理・rotation・usage粒度の評価は既存効率checkpointへ明記した。
- 未測定：実運用cohortのCodex Reduction、live provider挙動、OS電源断耐性。静的レビューの完了とこれらの実証完了を区別する。全Goテストの終了待ちを計画化の条件にしない。

## 引継ぎ時の作業範囲

レビューはこの会話の担当モデル自身が続行する。他モデルへレビューを委譲しない。Limitで中断した場合、後続モデルが担当するのは保存済みレビュー結果のコミットだけであり、再調査・追加レビュー・実装を担当させない。最優先は検証可能なFindingの発見と保存であり、Task形式への整形・Git操作・Guard通過を先行させない。全体レビュー完了は未宣言。

## 2026-09-23 追加Finding（再現確認済み）

### F6 / P2: CLI installerも更新中断後に通常再実行できない

- 対象: `glm-worker/internal/cliinstall/install.go:247` の `applyInstall` と同fileの `commitActions`。Codex設定installerとは別owner・別production経路。
- 発生条件: 既存v1を正規Install後、v2の全binaryを置換し、ownership stateのrename前にprocessが終了する。
- 原因: staged replacementとbackupはあるが、適用済みbinaryと旧manifestの組合せを次回Installが復旧するdurable transaction記録がない。排他lockはprocess終了で解放されても、この不整合を修復しない。
- 影響: 次回Installが自身の更新を `installer-owned binary content changed externally` と判定し拒否する。運用CLIの更新を正常経路で完了できず、手動修復が必要になる。
- 再現: 一時build/binでv1をInstall。子processでproductionのplanInstall → stageActions → commitActionsを呼び、manifest保存前相当でexit 77。親から通常Installを呼ぶと上記errorになる。全Install入口への非同期killではなく、実際のmutation関数を用いたdurable prefix再現である。
- 証拠: `/private/tmp/codex-repository-review-extended/cli_audit_test.go` の `TestAuditCLIInterruptedUpgradeCanRetry`、`installer-extended.jsonl:5`。
- 修正方向: current transactionの中断を所有権付きで復旧する。旧schema互換や、manifest削除による無条件上書きは追加しない。

### F7 / P2: CLI配置検証がowned executableのmode異常を成功扱いする

- 対象: `glm-worker/internal/cliinstall/verify.go` の `verifyExpectedBinary` と `install.go` の `requireOwnedTarget`。
- 再現: 正規Install後、owned `glm-worker` をchmod 0777。Verifyはnilを返す一方、同じ現物に対するInstallは `installer-owned binary shape or mode changed externally` として拒否する。
- 原因: Install/Retireのownership判定は0755を要求するが、Verifyはregular executableであることとcontent hashだけを見る。
- 影響: runtime配置検証が、installer自身が正常所有物として更新できないmodeのbinaryを成功扱いする。0777の実悪用や第三者による書換えを観測したという意味ではない。
- 証拠: `cli_audit_test.go` の `TestAuditCLIVerifyRejectsOwnedModeDrift`、`installer-extended.jsonl:10`。
- 修正方向: owned binaryのVerifyを正規ownership条件と整合させる。同一内容のunowned executableを許可する既存の責務と混同しない。

### F8 / P1: Claude settings mergeも管理外の同時編集を消す

- 対象: `glm-worker/internal/settingsmerge/merge.go` の `mergeFilesWithWriter` と `transaction.go` の `applyMergeTransactionPlans`。
- 発生条件: targetを読んでmerge結果を作った後、targetへのwrite直前にユーザーが管理外keyを編集する。
- 再現: targetを `{"user_key":"initial"}`、fragmentを `{"managed_key":"new"}` とする。production write callbackの直前にtargetを `{"user_key":"concurrent"}` へ変更し、通常writeAtomicを継続するとmergeは成功しuser_keyがinitialへ戻る。
- 原因: durable journalは中断後の復旧を保護するが、通常applyでは現在のtargetと計画作成時の内容を比較せず、古い全体を置換する。journalがあることだけでは通常実行中の編集保護にならない。
- 影響: installer管理外のClaude設定が失われる。Codex TOML installerの既存F1と同じinvariant違反だが、修正owner・呼出経路は独立する。
- 証拠: `/private/tmp/codex-repository-review-extended/merge_audit_test.go` の `TestAuditMergePreservesConcurrentUnmanagedEdit`、`installer-extended.jsonl:18`。
- 修正方向: normal apply・rollback・recoveryで外部編集を保護し、同一targetへの並行mergeを直列化する。lockだけで任意のeditorとのraceが消えるとは扱わない。

上記3testは実model・本番設定を使わず、一時directory内で期待する安全動作に対してFAILした。現在は既存5件と合わせて8件の具体的不具合を保存済み。追加3件の個別Task化は後回しとし、この記録を一次Findingとして保持する。

### F9 / P1: source evidenceのdedupが別path・別rangeの必要証拠を消す

- 対象: `glm-worker/internal/app/parent_evidence.go` の `projectSource`（872行付近）、`degradeDuplicateParentEvidenceParts`（295行付近）、`parentReviewEvidenceClaims`。
- 原因: sourceのDigestは抽出文字列のSHA256だけで、path・line rangeを含まない。dedup keyもsource surfaceとDigestだけ。同じbyte列を持つ別の位置が同一証拠とみなされる。
- 再現: `review.go:1` と `other.go:1` が両方 `package review` で、両pathをreview targetとする。両sourceを同じmanifestで要求すると2つ目のContentが消され、target全件のproofを生成できない。
- 影響: 正しい別位置の証拠を一度ずつ求めてもreview acceptanceへ進めない。回避のために不要な周辺行を増やす等の親作業・token消費を発生させる。本文転送の重複排除と、証拠が対象をカバーするidentityを同一視している。
- 証拠: `/private/tmp/codex-repository-review-extended/evidence_audit_test.go` の `TestAuditIdenticalSourceAtDifferentPathsProvesBothTargets`。`evidence-reproduction.jsonl:4` に `two distinct source targets were requested but content dedup leaves no proof`。
- 修正方向: locator/snapshotを含む証拠identityを保持する。同じ本文の再転送を省くなら、配信済み本文への参照を各対象のproofとして機械的に検証する。review guardの単純削除・旧ledger互換は行わない。

### F10 / P1: 分割配信したreview証拠を合算できず、再取得もdedupされる

- 対象: 同fileの `markParentReviewEvidenceProof`（941行付近）、`parentReviewEvidenceClaims`、`saveSurvivingParentEvidenceClaims`。
- 原因: proofは今回のresponseのpartsだけで全targetを満たす時しか保存しない。一方、個々の配信済み内容は即座にdedup ledgerへ記録する。正当に配信済みのpartial coverageを次callのproofへ引き継ぐ経路がない。
- 再現: 同じreview/lease/snapshotでtargetを `review.go:2` と `other.go:2` にする。別々のevidence callで各sourceを正常配信し、その後両方をまとめて再要求する。全件が配信済みとして除去され、proofは依然nilとなる。F9と異なり、この2行の内容は互いに異なる。
- 影響: bounded readやbudget refinementに沿って分割した読み方がreview採否に結び付かない。全件同時配信を暗黙要求し、重複読取禁止と衝突する。target全体がbudgetに収まらない時ほど問題になる。
- 証拠: `evidence_audit_test.go` の `TestAuditEvidenceReadAcrossBatchesCanCompleteReview`、`evidence-reproduction.jsonl:9` に `both targets delivered in same lease, combined retry deduplicated, review proof still missing`。
- 修正方向: 同一review/lease/snapshotへbindした配信成功のcoverageだけを蓄積し、全target到達時に採否を許可する。別review・古いsnapshot・省略されたbody・配信失敗を引き継がない。巨大一括readや重複再投影を解決策にしない。

F9/F10はproductionのprintParentEvidenceから既存review fixtureへ通す一時testで再現済み。現在の具体的不具合は10件。source/testのproduction修正はしていない。

### F11 / P1: 正規のreview target表現でも、新規symbol・削除行の証拠を採否へ結び付けられない

- 対象: `glm-worker/internal/app/parent_review_evidence_projection.go` の `buildParentReviewEvidenceManifest`、`parent_evidence.go` の `captureParentEvidenceDiff` / `parentReviewSourceCoversTarget` / `parentReviewDiffHunkCurrentRange`。
- 契約: reviewer promptは `file:symbol/行範囲` の最小対象を要求し、packet validatorもこの用途を受理する。
- 再現A: 新規untracked `new.go` に `func NewAPI() {}` を置き、targetを `new.go:NewAPI` とする。自動evidenceはsymbolをdiffへ変換するが `git diff HEAD` はuntracked fileを含まない。追加で全sourceを配信しても、sourceのcoverage判定がnumeric locatorしか認めず、accept-readyはfalseのまま。
- 再現B: tracked `review.go` を削除し、targetを `review.go:2` とする。自動evidenceは存在しないcurrent sourceを要求する。追加で削除diff全体を配信しても、coverageがnew-sideのhunk行範囲だけを評価し、全削除の `+0,0` を拒否するためaccept-readyはfalseのまま。
- 影響: 新規APIや削除の意味判断という通常の高リスクreviewで、必要な証拠を読んでも機械上の採否へ進めない。targetを書き直したpacketの再発行等、不要なmodel/親往復が必要になる。意味判断を省いてacceptさせる修正では解決しない。
- 証拠: `/private/tmp/codex-repository-review-extended/target_audit_test.go`。`targets-usage.jsonl:22` は `TestAuditReviewUntrackedSymbolCanBeProven`、27行目は `TestAuditReviewDeletedNumericTargetCanBeProven` の再現診断。既存fixtureで変更後snapshotにreviewをbindし、production evidence入口とParentReviewAcceptReadyを通した。
- 修正方向: target locatorの意味とevidenceのold/current sideを明確にし、untracked sourceと削除diffを正規proofへ結び付ける。証明不能なlocatorを受理したまま親へ無限に再読を要求しない。旧schema互換や推測によるacceptは追加しない。

### F12 / P2: A/B評価が欠落・nullのtoken値を「実測100%削減」と報告する

- 対象: `glm-worker/internal/abeval/abeval.go` の `CodexUsage` / `Known`、`validate.go` の `LoadRecord` / `validateCodexUsage`、`compare.go` の `codexReduction`。
- 原因: input/outputを非pointer整数へdecodeするため、欠落・nullと明示0を区別できない。Knownはsource文字列の有無だけを調べ、validationもtoken fieldの存在を要求しない。
- 再現: validOrchestratedRecordのcodex_usageを `{"source":"codex-app-usage-export"}` に置換する。input_tokens/output_tokensを両方nullにした場合も同じ。LoadRecord→ValidatePairが成功し、正常なdirect記録とのBuildReportはstatus=actual、input/output reduction=100となる。
- 影響: 未観測値がCodex Reductionの改善として計上され、最上位評価を誤らせる。実運用の評価結果が既に汚染されたとは確認していない。
- 証拠: `/private/tmp/codex-repository-review-extended/usage_audit_test.go` の `TestAuditMissingUsageCannotBecomeActualReduction`、`targets-usage.jsonl:9,14`。診断は `unobserved token fields accepted: status=actual input_reduction=100 output_reduction=100`。
- 修正方向: current schemaでknown usageの数値fieldの存在・非nullを検証し、不完全な記録を拒否またはunknownとして保持する。明示0との区別を保ち、既存の欠落記録を0へ補完する互換処理は入れない。cache/reasoning粒度を拡張する議論とは独立に修正可能。

再開時の追加確認: Codexのinstalled instruction参照は `codexinstall/files.go:31` で `codex/AGENTS.md` から明示配置されていた。source treeに同名instructionが無いことだけを欠損Findingにはしていない。追加の3 top-level test（A/Bはmissing/nullの2 subtest）が期待する正常動作に対してFAILし、具体的不具合は12件となった。GLM・他モデル・Git作業・production修正は行っていない。

### F13 / P1: quality surface保護からShellCheckの自動読込設定が漏れている

- 対象: `glm-worker/internal/workflow/selfprotection.go:110` の `IsQualitySurface`、`quality_gate.go:37` の `captureQualitySurfaceDigest`、`harnesslint/external.go:214` のShellCheck呼出し。
- 原因: quality surfaceを固定のpath/prefixで列挙する一方、ShellCheckは `-f gcc <path>` で実行し、自動検出される `.shellcheckrc` を無効化も保護対象化もしていない。
- 再現: markerを有効にした一時repositoryで `#!/bin/sh\necho $1\n` にShellCheck 0.11.0がSC2086を報告することを確認。`.shellcheckrc` に `disable=SC2086` を追加すると同じcommandが成功するが、productionのquality surface digestは変更前後で一致した。
- 影響: 通常workerが品質判定設定を変えた際の親承認境界を経ず、実際のlint判定を弱められる。後段の独立reviewやcritical-path判定まで全て回避したという証明ではない。保護対象の列挙とexternal toolの暗黙入力が一致していない責務上の欠落である。
- 証拠: `/private/tmp/codex-repository-review-extended/quality_surface_audit_test.go`、`gate-boundary.jsonl:5`。実際に配置済みのversion固定ShellCheckを一時fileへ実行し、digestはproduction関数を使用。
- 修正方向: 許容するtool設定入力を決め、実際のconfig探索範囲と保護scopeを揃える。必要に応じ暗黙設定探索を無効にする。単に1つのfilenameを追加して全external toolの入力が保護されたとは扱わない。新しいLLM guardは不要。

### F14 / P2: quality gateの重複実行抑制が異なるGo moduleの検証を取り違える

- 対象: `glm-worker/internal/app/quality_gate_execution.go:61` の `startQualityGate`、`quality_gate_persistence.go:88` の `findRunningQualityGateRun` / `sameQualityGateSnapshot`。
- 原因: 同時実行をまとめるidentityはform・repository・HEAD・index/worktree digestだけで、実際の `go test ./...` の対象を決めるWorkingDirを含まない。
- 再現: 同一repositoryにmodule-a/module-bを用意し、module-aのrunning記録を保存する。同じsnapshotのmodule-bをcwdとしてproduction startQualityGateへ要求するとmodule-aへattachする。Aの終了を通知するとBの要求はstatus=pass・working_dir=module-aとして成功し、Bのrunnerは起動しない。
- 影響: 複数moduleや異なるpackage directoryの並行検証で要求対象のtestが実行されない。worker parent-validation経路は返却WorkingDirを後段で再検証するためそこで拒否できるが、startQualityGate自体は誤った成功を返し、少なくとも正常な検証の完了を妨げる。
- 証拠: `/private/tmp/codex-repository-review-extended/gate_identity_audit_test.go`、`gate-boundary.jsonl:13`。fixtureのrunner終了だけを置換し、productionのstart・persist・coalesce・outputを実行した。実Go suiteを2本走らせた再現ではない。
- 修正方向: 正規化したWorkingDirを実行identityへ含め、同じcommandの同じ対象だけを共有する。現行snapshot/対象の完全一致を維持し、曖昧な別moduleのPASSをfallbackにしない。

具体的不具合14件を保存。今回の再現はGuard通過を目的とした実行ではなく、レビュー対象の判定関数・CLI経路の局所検証である。

### F15 / P2: 自動再開のinventoryが無関係なautomationへwake専用規則を適用して停止する

- 対象: `glm-worker/internal/autoresume/codex_wake_transaction.go` の `resolveCodexWakeAutomation`、`coalesce.go` の `readWakeTOML`、`toml.go` の `validateAutomationFields`。
- 原因: 全automation directoryをwake専用readerへ渡し、targetが一致するか調べる前にname=directory名とtarget_thread_idの存在を要求する。対象wakeのidentity規則がユーザーの全automation inventoryへ漏れている。
- 再現A: 別threadをtargetに持つ `daily-check/automation.toml` のnameを `Daily checks` にする。BuildCodexWakeTransactionはnameとdirectoryの不一致でerrorとなる。
- 再現B: 無関係なcron形式としてtarget_thread_idを持たないautomationを置くと、同入口がmissing required fieldでerrorとなる。空inventoryでは既存testどおりwake準備が可能。
- 影響: 当該wakeの重複や不整合がなくても再開予約の準備ができず、Limit時の親作業・復帰を妨げる。実schedulerへのwrite・予約・削除は行っていない。
- 証拠: `/private/tmp/codex-repository-review-extended/wake_audit_test.go` の `TestAuditUnrelatedAutomationDoesNotBlockWake`、`wake-boundary.jsonl:6,11`。正規reset fixtureとproductionのBuild入口を使用。
- 修正方向: 汎用inventoryから対象を識別する読取と、当該wakeに必要な厳密identity検証を分ける。同じtargetへの重複は検知しつつ、無関係なユーザーautomationへwakeの命名規則を要求しない。旧automation形式を推測して受理する互換層ではなく、所有scopeの修正とする。

具体的不具合15件を保存済み。今回再開分はF11〜F15の5件で、計6 top-level test・4 subtestの再現診断を保存した。後続モデルが記録をコミットする場合もレビュー自体を委譲しないという範囲を維持する。

### F16 / P1: 開始時から存在するuntracked fileの変更・削除がtask diffから消える

- 対象: `glm-worker/internal/state/baseline.go:32` の `CaptureGitBaseline`、`taskdiff/capture.go:200` の `taskCreatedPaths` / `taskCreatedUntrackedPaths` / `taskCreatedTrackedPaths`。
- 原因: baselineはuntracked fileのpath集合だけを記録し、内容・modeを保存しない。task diffはその集合に含まれるpathを一律除外するため、開始後に変更されたかを判定できない。
- 再現: tracked seedに加え、開始前からuntrackedの `preexisting.go` を置いてproduction CaptureGitBaselineを呼ぶ。そのfileの関数を変更、またはfileを削除すると、ChangedPathsは空集合、Captureはavailable=trueで0 byteのdiffを返す。どちらも既存test fixtureのproduction経路で確認した。
- 影響: reviewerへ「wrapper-baseline-to-review-start」の正本として渡すpatchから、実際にworkerが行った変更が抜ける。workflowの保守的なpath追加はまだ存在するuntracked fileを拾えるが、正確なpatchを復元できず、削除されたuntracked fileはその追加列挙にも現れない。reviewerが実際に見逃したというmodel実験ではない。
- 証拠: `/private/tmp/codex-repository-review-extended/taskdiff_audit_test.go`、`taskdiff-boundary.jsonl:6,11`。診断は両caseで `paths=[] diff_bytes=0`。
- 修正方向: 開始時untrackedの必要な内容・種別・modeを正規baselineとしてbindし、その後の変更・削除を比較する。開始前からの無変更fileはtask差分へ混入させない。既存baselineに情報がない場合に推測復元する旧schema互換は入れず、未証明状態を明示する。

具体的不具合16件を保存済み。今回再開分F11〜F16は6件、再現は7 top-level test・6 subtest。総合レビューの12観点を扱っているが、全source行・全crash地点・全外部環境の検証完了を意味しない。

## Codex Reductionに向けた現在の優先判断

- F9/F10/F11: bounded evidenceを正規に読んだ後の採否不能を解消する。親の再読・packet再発行・同じ判断のやり直しを減らす直接候補。回数や削減率は未測定。
- F16/F13: 差分の欠落と品質設定の保護漏れを先に解消する。これらが残った状態でreviewやtestを減らして品質非劣化を主張しない。
- F12: 不明なusageを改善率に変換しない。実施策の削減率を比較する前提となる測定不具合として、cache/reasoningの粒度拡張と切り離して扱う。
- F14/F15: 自動化のidentityと所有scopeを正しく絞る。同一対象の重複処理は機械でまとめ、異なる対象やユーザーの無関係な設定を混同しない。
- 既存のquality二重実行解消、F2のfix/review snapshot整合、F5の停止可能なprobeも継続候補。新しい汎用guardや親の手動チェックを増やすより、既存の機械処理が生む手戻りを減らす。

未検証の範囲は実task cohortのReduction/Quality Delta、live provider・scheduler・本番install、全state書込地点のcrash/race、全security経路。今回のレビューはこれらの実証完了を宣言しない。
