# 実装task lifecycle規則

このfileは長期間変わらないtask lifecycle、ownership、compaction、commit、review、exceptional decision記録の規則だけを保持する。個別task仕様は書かない。

## source-of-truth順位

作業開始・再開時は次の順を正とする。

- Git / working treeの現物
- `IMPLEMENTATION_RULES.md`
- `IMPLEMENTATION_PLAN.local.md`
- Planが示すACTIVE `IMPLEMENTATION_TASKS/*.md`
- ACTIVE taskが明示参照する`IMPLEMENTATION_HISTORY.md`のexceptional decision見出し
- conversation context、compaction summary、internal TODO

conversation context、compaction summary、internal TODOはsource of truthではなくcacheとして扱う。Git現物とPlan/Taskが矛盾する場合、親Codexが現物確認後にPlan/Taskを修正してから続行する。

## 要求受領とtracked化

- すべての新規ユーザー要求はconversation contextだけに保持したまま作業を続けず、compaction、GLM call、長時間調査、実装開始より前に親Codexがtrackedな正へ固定する。「後でPlanへまとめる」は禁止する
- tracked化は独立task作成やACTIVE切替と同義ではない。要求内容をparent-managed surface自体へ完全に表現できるか、現ACTIVEの同一責務か、独立taskかを別々に判断する
- 現ACTIVEそのものへの追加指示は、新task分離を判断する前にACTIVE taskの`Amendments`へ原文を時系列追記する。同一責務のacceptance追加は同じACTIVEで継続し、独立責務は新taskへ分割し、別問題を優先実装する場合だけPlan上のACTIVEを切り替える
- 各taskの`Original instruction`はimmutableなlossless requirement sourceとし、契機となったユーザー/親Codexのtask該当指示を可能な限り原文のまま保存する。要約、重複除去、理由の省略、実装TODOだけへの圧縮、「意味は同じ」という書換えを禁止する
- 追加要求は旧本文を上書き・削除せず、日時または順序と原文を`Amendments`へappend-onlyで追記する。新旧要求が矛盾する場合も両方を保持し、最新Amendmentによるoverrideをderived `Contract`へ明示する
- 「これ」「さっきの」「前のreview」等の会話依存参照はOriginal instructionを書き換えず、当時の解決結果を`Resolved references`へ分離して具体化する
- task fileへ進捗日記を追加せず、requirement contractと最小の`Current boundary`だけを保持する。長い診断とruntime/model evidenceはartifact / bundle / telemetry、ordinary completionのcommit・diff・validation・install evidenceはGit / CI / bundleを正とし、Historyへ複製しない

## lossless sourceとderived contract

- `Original instruction` / `Amendments` / `Resolved references`を一次要求source、`Purpose` / `Contract` / `Must not` / `Acceptance criteria`を実装・review用のderived informationとして明確に分離する
- derived sectionが十分詳細でもOriginal instructionを要約してよい理由にはしない。token削減はderived sectionと通常resume経路で行い、一次要求sourceを削らない
- 長い指示を複数taskへ分割する場合、各taskの要求・理由・禁止事項・完了条件を理解するために必要な原文sectionをlosslessに保存する。共通前提を暗黙依存にせず、必要部分を含めるかtrackedな共通sourceを明示参照する
- derived sectionへOriginal instruction全文を複製せず、compactな作業・review indexとして維持する
- RULES変更が既存taskへretroactiveに影響する場合はmigration要否を判定し、矛盾するtask metadataを更新して同じcontractで実行可能なことを確認してから完了する

## parent maintenance

次をすべて満たす変更は、独立taskを作成・ACTIVE化せず、現在のACTIVEを維持したまま親Codexが直接処理できる。

- production behaviorとimplementation codeを変更しない
- GLM workerへの独立実装委譲、独立した長時間調査、通常実装task相当のreview/testを必要としない
- 現ACTIVEの意味契約・acceptance criteriaを変更しない
- 独立したrollback単位として管理する必要がない
- Rules、Plan、Task metadata、必要時のexceptional History decision等のparent-managed surfaceだけで完結する
- 変更後の意味契約をそのtracked surface自体へ完全に保存できる

命名規則、Plan priority/ACTIVE/NEXT metadata、typo、意味を変えない明確化、exceptional decision recordの参照修正、意味契約を変えないAmendment、parent-managed metadataの参照修正は代表例とする。parent maintenance中もACTIVEは主要な実装・調査・review対象を示し続け、一時task作成、ACTIVE退避、maintenance完了処理、元ACTIVE復帰を行わない。

parent maintenanceは記録不要を意味しない。ユーザー要求をcompaction前に対象のRules / Plan / Taskへ直接保存し、内容に応じた最小確認を行う。Historyへ追加できるのは、production diffやcurrent Rules/taskだけでは表現されず、将来のtracked taskがactivation / adoption条件として明示参照するcross-taskの採否・Go/No-Go decisionだけとする。通常の作業・完了chronology、commit、validation、install、escaped defect診断をHistoryへappendしない。変更が単独で意味を持ち、即時固定が安全なら小さな独立変更として記録できる。GLMへcommit/pushさせない。

parent-managed metadataを扱うguard、self-protection、production wiring自体の変更は、編集対象がparent-managed surfaceに関係していてもproduction behavior変更であるためparent maintenanceにしない。

## 通常taskへの分離

次のいずれかに該当する要求はparent maintenanceにせず、内容を表すsemantic slugの独立task fileへ固定する。

- production code、CLI / API / protocol behavior、state / checkpoint / telemetry semanticsを変更する
- worker / reviewer promptまたはproduction wiringを意味的に変更する
- test / integration scenario追加が主要成果になる
- 独立reviewが必要な設計変更、複数fileにまたがる実装責務、長時間調査を含む
- 現ACTIVEとは独立したacceptance criteriaを持つ、または途中実装でrollback境界が曖昧になる
- ユーザー許可待ちを独立管理する

独立taskを作成しただけではACTIVEを自動変更しない。現在の主要作業より優先する根拠がある場合だけPlanのACTIVE / NEXT / BLOCKEDを更新する。

## task file必須構造

全task fileは最低限、lossless sourceである`Original instruction`、append-onlyの`Amendments`、必要時の`Resolved references`、derived informationである`Purpose`、`Contract`、`Must not`、`Acceptance criteria`、および`Historical invariants`、`Dependencies`、未解決時の`Review findings`、`Current boundary`を持つ。schedule stateはPlanだけを正とし、task fileへ`Status`を持たせない。Goal modeで成功済みdependency edgeを保持する必要があるtaskだけはoptional `Fulfilled dependencies`を持てる。欠落はfulfilledなしとして扱う。

## task filename

- 新規`IMPLEMENTATION_TASKS/*.md`は内容を表すsemantic slugだけをfilenameに使用し、sequence、priority、status、dependency、completion順、作成順、permission wait分類を表すnumeric prefixを付けない
- 実行順序とpriorityは`IMPLEMENTATION_PLAN.local.md`のACTIVE / NEXT / BLOCKEDを正とし、dependencyは各task fileの`Dependencies`へpathで明示する。filenameや番号大小から推論しない
- 割り込みtaskもsemantic filenameを追加してPlan上の位置だけを変更する。順序へ割り込むための番号、枝番、BLOCKED専用番号帯を作らない
- この規則導入前から存在する番号付きtask fileはrenameせず、reopen時も既存filenameを維持する
- numeric prefixを禁止するためだけの複雑なvalidatorは追加せず、新規taskを作る親instructionと生成経路でsemantic filenameを固定する
- Planを含むtask scheduling listはunordered marker `-`を使い、source上の行順をpriorityとする。割り込み時は項目の移動・追加だけを行い、numeric markerを付けない

## task粒度とdispatch

- 原則は1 task file = 1つの独立review可能な変更責務 = 1個別commit候補
- acceptanceが独立、別commit可能、protocol/transport/observability/Eval等で責務が異なる、個別rollback可能、一方だけpermission待ち、1 worker callへ複数大規模責務を渡す場合は分割する
- 同一invariantを成立させる実装とintegration testは過剰分割しない
- umbrella見出しをそのままGLMへ渡さず、具体task file 1件だけをdispatchする

## parent orchestrationのproduct化判断

- 親Codexは通常作業中、自身が同種の手作業・定型操作・定型判断を繰り返していると気付いた時点で、それを親の手作業に残すべきか、glm-workerのcommand / machine interface / automationへ取り込むべきかを判断する。対象は実行制御、状態確認、復旧、安全確認、review補助を含み、command追加に限定しない
- 反復可能で機械的に強制でき、Codex / Sol消費または反復障害を減らし、実装・保守コストに見合うと判断した場合は、ユーザーの個別指摘を待たず通常のPlan lifecycleへsemanticな改善taskを追加する。一度限り、低頻度、実装コストに見合わない候補、および高レバレッジな意味判断は機械化しない
- 判断対象は親の手作業だけでなくworker / reviewer / test / build / lint / smoke / provider probe / polling / resume verification等のmachine executionを含む。同一または実質同一の高コスト処理がtask wall-clockの主要部を占める一次証拠を得た場合、worker/reviewerは現taskを勝手に縮退・拡張せず観測を重複なく報告し、親Codexが再発性、coverage維持、expensive real executionとcheap contract verificationの分離、費用対効果、false success・flakiness・観測不能riskから独立task化を判断する
- 各taskの実行中に、Codex / Solの実消費、Quality Delta、不要なparent return、停止、retry、poll、再説明、重複validation、過大なmodel-visible output、観測欠損等の改善候補を新たに得た場合、親Codexはその意味のある状態遷移で再発性・削減効果・品質risk・実装費用を評価する。改善価値があれば現在taskへ混ぜず、現在task完了を待たずにparent-managedな独立taskとして022より前のPlanへ追加し、通常のGo/No-Goと作業サイクルへ含める。現在taskを中断して優先するのは、継続すると証拠喪失・品質低下・大幅な追加消費が生じる場合だけとする
- 意図どおり作動した安全停止、必要なSol semantic gate、単発の外部limit、測定可能なCodex削減または品質維持へ結び付かない違和感は、発生しただけで改善task化しない。ただし同じ境界が反復してCodex / Sol消費の主要因になる、または品質を保ったままround tripを削減できる一次証拠が得られた場合は再評価する
- 105後・022前のCodex効率再評価はこの随時判断の代替ではなくsafety netとする。同評価では、それまでのtask packet、telemetry、Codex rollout、停止/retry/fix/review/validation履歴を横断し、作業中に観測したがTask化しなかった候補を含めて取りこぼしがないか再精査する。新しい実行可能Findingがあれば022より前へ追加し、その完了後も必要な再評価を続ける
- 初回棚卸しtaskを設けても、その完了をこの継続的判断義務の完了とは扱わない。この規則は通常orchestrationの恒久contractとして残す

## Goal起点のproject orchestration

- Plan管理repositoryは`IMPLEMENTATION_PLAN.local.md`のoptional `## GOAL`節を、project-levelのlosslessなGoal原文、append-only amendment、derived acceptance、bounded completion decisionの正本として使える。GOAL節がない既存repositoryのschedule・task lifecycle・CLI behaviorは変更しない
- Goal受領時の初期task生成、priorityとdependencyに基づく選択、worker/reviewer結果・finding・user amendmentによる再計画、Goal acceptanceは親Codexのsemantic authorityとする。通常利用者へPlan fileの逐次編集、task選択、通常resume操作を要求しない
- `glm-worker --project-state`はmodel呼出・state変更・repository lockを行わないread-only machine projectionとし、GOAL検証、schedule、dependency graph、next runnable、blocker、mechanical completion readinessを返す。Plan/Task編集、task昇格、replanning、completion acceptanceを行わない
- task fileの`Dependencies`にあるpathはoutstanding、`Fulfilled dependencies`に親Codexが明示移動したpathだけをfulfilledとする。同じpathを両sectionへ置かず、current treeからtask fileが消えたこと、Git履歴にそのpathが存在すること、commit message・時刻・filename近接だけからfulfilledを推定しない。`Dependencies`のpathがcurrent treeに存在せずfulfilledにも明示されていない場合はunknownとしてfail closedする
- prerequisite taskが通常のsemantic acceptance・必要validation・親action完了を経て成功完了した場合だけ、task file削除とschedule同期の前に、残存する各dependent taskでそのpathを`Dependencies`から`Fulfilled dependencies`へ親Codexが同一metadata同期として移す。No-Go、cancel、withdrawal、replanによる単純削除はfulfilledへ移さず、dependent側の要求自体を変更する必要がある場合は通常のtracked amendment/replanningとして扱う
- self dependency、outstanding edgeのcycle、malformed GOAL / schedule / task contract、明示状態の矛盾はfail closedとし、`runnableなし`や`complete`へ縮退しない
- Goal進行中は既存どおりACTIVE taskを一意に要求する。mechanical completion readinessは、current ACTIVEのlifecycleがcompleteかつpending parent actionなし、NEXT / BLOCKEDが空、unresolved findingなし、current snapshot対応validation成功、clean working treeを最低条件とし、semantic Goal acceptanceと必要なinstall判断を代替しない
- 親Codexがmechanical readinessとGoal acceptanceを確認してbounded completion decisionをGOAL節へ記録したterminal Goalだけは、ACTIVE / NEXT / BLOCKEDが空のPlanを許可する。未完了GoalでACTIVE空を許可せず、単一worker PASSをproject completionへ昇格しない
- 既存task state、checkpoint、parent action、worker/reviewer、Codex gate、rate-limit / Codex-limit recovery、telemetry、Plan/task guardを再利用し、Goal mode専用daemon、scheduler、state DB、第二正本を追加しない

## priorityとhard dependency

- PlanのACTIVE / NEXTにおけるsource上の順序は変更可能な実行priorityだけを表す
- task fileの`Dependencies`は、そのtaskを正しく実装・検証するために先行taskの成果物が実際に必要なcorrectness prerequisiteだけを、`IMPLEMENTATION_TASKS/<semantic-or-existing-name>.md`形式のpathで明示する
- Planで先行予定、作成順、番号大小、隣接taskという理由だけでdependencyを追加しない。priority変更はPlanだけを変更し、hard dependencyと同一視しない

## 再読contract

新session、compaction後、rate limit/provider-unavailable後、長時間停止後、user追加指示後、`--resume`、internal TODO不一致時、reviewer差戻し後、false-complete再open時、acceptance最終確認時、ユーザーから指示適合性を確認された時は、コードへ触る前に次を読む。

- `IMPLEMENTATION_RULES.md`全文
- `IMPLEMENTATION_PLAN.local.md`全文
- ACTIVE task file全文（Original instruction、Amendments、Resolved referencesを省略しない）
- ACTIVE taskが明示参照したexceptional History見出しだけ

NEXT taskは開始時まで全文を読む必要はない。

## worker / reviewer contract

- workerとreviewerは同じACTIVE task fileを要求定義として独立に読む
- reviewerは開始時にimplementer summaryだけでなくOriginal instruction、Amendments、Resolved references、Contract、Must not、Acceptance criteriaを確認する
- reviewerは`実装 vs Contract`だけでなく`Contract vs Original instruction / Amendments`も比較し、derived contract作成時の要求欠落自体をreview対象にする
- scripted runnerの期待packetだけでなくproduction prompt/dispatchとの因果をtestで固定する
- review結果はdefect、user-visible/workflow impact、why Codex+GLM missed it、要求由来/実装由来複雑性、preventionを区別する。原因層はparent orchestration、requirement preservation、worker、reviewer、Sol gate、production wiring、test/scenario、cross-cutting invariant compositionから一次証拠で分類する

## blocked taskのactivation

- BLOCKED / USER_PERMISSION_WAIT taskはpermission受領だけでACTIVEへ移さない
- 許可原文を`Amendments`へlosslessに追記し、prerequisite evaluation artifactを読んだ上で、concrete `Contract`、`Must not`、`Acceptance criteria`を確定してからACTIVE候補にする
- placeholder contractのままGLMへdispatchしない

## 親Codex専有metadata

次を親Codexだけが編集するtracked surfaceとする。

- `IMPLEMENTATION_RULES.md`
- `IMPLEMENTATION_PLAN.local.md`
- `IMPLEMENTATION_TASKS/*.md`
- `IMPLEMENTATION_HISTORY.md`

GLM worker/reviewerは編集・生成・復元・削除せず、更新候補をstructured resultで返す。model実行中は不変、model停止中の親更新だけをparent-managed deltaとして扱い、worker/reviewer implementation surfaceの外部変更はfail closedにする。pathごとの分岐を増殖させずparent-managed implementation metadataの単一集合へ収束する。Historyがこの集合に残る理由は、current BLOCKED taskから明示参照される少数の非diff decisionを保護するためであり、通常completion ledgerへ戻す理由にはしない。

## task完了

ordinary task完了時はHistoryへ完了証跡やescaped原因を追記しない。Goal modeでそのtaskをhard prerequisiteとして参照する残存taskがある場合は、成功完了が確定した同じparent metadata同期で該当edgeをdependent側の`Dependencies`から`Fulfilled dependencies`へ移してから完了task fileを削除する。その後Planからentryを削除してNEXTをACTIVEへ昇格し、final HEAD上でPlan・ACTIVE file・Git境界が一致することを機械確認する。完了task fileを`IMPLEMENTATION_TASKS/`へ残さない。Git履歴が原要求と実装diffを保持し、CIとbundle / telemetryがvalidation・runtime/model evidenceを保持する。

Goal modeの最終taskだけは、上記Goal起点project orchestrationのterminal条件を満たす場合に限り、NEXT昇格ではなくcompleted GOALと空scheduleへ同期する。Goal未完了、mechanical readiness未充足、semantic acceptance未確定ではこの例外を使わない。

Historyを更新できるのは、完了結果そのものではなく、そのtaskがproduction diffへ表現されないcross-task decisionを新たに確定し、将来のtracked taskがそのdecisionをactivation / adoption条件として明示参照する場合だけとする。その場合もdecision、最小根拠、再評価境界だけを残し、commit / validation chronologyや長い診断を複製しない。参照taskがなくなったrecordは削除する。

## install / validation

- GLMにcommit/pushさせない
- task metadata同期はfinal HEADの機械postconditionを正とし、文書手順だけで保証したことにしない
- runtimeへ影響するtaskはimplementation、test/review後、適切な区切りで`install.sh`本配置、installed/source一致、そのinstalled状態で必要なproduction smokeまでをtask completion flowとして行う。複数task分を未配置のまま後続実運用へ進めず、最終taskまでinstall義務を延期しない
- source-only metadata変更はruntime install対象から除外する

## Greptile日次review

- Greptile日次reviewは本repository固有の補助review orchestrationであり、`glm-worker` production機能へ統合しない
- 日次schedulerは`codex-config` project所属のLuna Low専用taskだけを起動し、review入力precondition、Standard CLI 1回実行、JSON/status検証、finding正規化、親実装taskへの1回送信、正常時のcheckpoint更新という機械処理だけを担当する
- 専用taskはfindingの最終採否、設計判断、自動修正、Task/Plan編集を行わない。findingの有効性・重複・Task化とGreptile継続利用価値は親CodexがGit現物と複数runの傾向から判断する
- findingがあるrunは親taskへの送信成功前にcheckpointを進めず、送信失敗時は次回runで最後の成功地点からcatch upする。finding 0件は親taskを起こさずcheckpointだけを正常更新できる
- scheduler移行・prompt変更の確認だけを理由にGreptile CLIを実行してcreditを消費せず、旧taskと新taskへautomationを二重登録しない

## repository automation authority

- ユーザーは親Codexがこのrepositoryに属するCodex app automationを作成・更新・削除することを恒久的に許可している。rate-limit自動再開、Codex limit wake、review scheduling等、現在taskまたはtracked repository workflowの実行に必要なautomationは、automation単位の追加確認を要求せず管理してよい
- 2026-08-31のユーザー指示により、この許可は特定日時・一回の発火・現在taskだけに限定しない。24時間継続して開発するという本repositoryの目的に沿って、現在および将来の許可済み開発taskについて、rate limit等による停止後の再開automationの作成・再作成・更新・再予約を恒久的に許可する。繰り返し制限に到達しても、同じ許可を日時や予約単位で取り直さない
- 再開後は、そのtaskに既に許可された実装・独立review・Sol判断・validation・Plan/task metadata同期・installまで継続してよい。各発火を別の未許可作業と扱わず、automation promptには本節の恒久許可と対象task・checkpoint・停止境界を明示する。一回限りscheduleは重複実行を避ける予約形式であり、ユーザー許可の有効期限を意味しない
- automation authorityはautomationのlifecycle管理だけを許可する。automation promptが後で実行できる操作は、その発火時に適用されるユーザー指示とrepository authorityのscopeを超えない。別repository、credential操作、購入、または無関係な外部変更へ拡張しない
- 一回限りの再開automationは保存済みtask ID・同一checkout・resume可能stateを発火時に再検証し、条件不一致ならresumeせず削除または停止する。重複予約を作らず、完了・別state移行後は不要なautomationを削除または停止する
- 恒久許可はtaskの開始条件・feasibility gate・品質gate・ユーザー指定の停止境界を解除しない。「現在ACTIVE完了後は次taskを開始せず停止」のような指示は引き続き守る。Codex app等の外部安全審査をこのfileで無効化・迂回せず、同じ許可済みscopeの拒否が再発した場合は、拒否理由と恒久許可の適用関係を確認し、実際に追加権限が必要な部分だけを報告する

### 恒久許可の原指示（2026-08-31）

直前の確認に対し、ユーザーは日時・現在taskに限らない恒久許可として次の原文を回答した。

```text
いや今後永続的に許可するよ
と言うかこの契約を無条件に作成させることできないの？
このリポジトリの目的のひとつに24時間連続して開発するツールであるということがあるんだがこのままだとその意味が消滅する
```

## machine-only data原則

glm-worker/Codex/GLMだけが生成・消費するmachine dataを長期公開APIとして扱わない。旧parser、migration、fallback、deprecated推定、version bridge、dual protocolを「一応読める」だけで恒久追加しない。current schema validationは厳格に保ち、old versionは用途に応じreject/skip/reset/rebuild/delete/resume不能を選ぶ。active task保護と恒久互換を混同しない。

## repository structure invariants

- Go commandは`cmd/<name>/main.go`を起動処理だけの薄いentrypointとし、feature固有のCLI分岐・command dispatch・設定解析・永続化・HTTP handlingを`main`へ置かない。実装責務は`internal/`配下のownerが持ち、この構造を`harnesslint`のentrypoint-thin ruleがAST構造判定で機械的に強制する
- `glm-worker`の成功stdoutは`--watch`のJSON Lines streamingを除き単一のmachine-readable JSON objectとし、失敗時はstdoutを空に保ちstructured error JSONをstderrへ出す。help/bootstrap等のearly-return pathも含め、この契約を`glm-worker`内のsingle-object validatorが実stdoutへのrelease前に機械的に強制する

## 禁止

- taskごとの独自state DB、filesystem watcher、daemonを追加しない
- task fileをHistoryや進捗日記にしない
- Planとtask fileへ詳細仕様を二重管理しない
- ユーザー許可のない実Sol High本番A/Bとbenchmark目的の追加AI callを行わない。GLM worker/reviewerへGit remote writeを許可しない
