# Codex効率中間評価（2026-11-12）

## 対象と評価方法

前回評価は `ef0b318494cce6a1119c467b1a6292ef114d1db8` の
`review-evidence/codex-efficiency-followup-2026-11-03.md`。
今回の既存実行証拠の上限は `0bb9e2dcb21a36f11f9a669f861dfb1a44a2e1cd`。
この間は72コミットであり、Task数とは同一視しない。
今回発見した開始projectionの不一致だけを本評価内で修正する。
親Codexが評価・採否・実装を担当し、評価目的の追加モデル呼出、本番A/B、CI照会は行わない。
修正を追加Taskへ起票せず、評価・修正・終了Metadataを1 Task 1 Commitへまとめる。

## 最上位指標と品質

Codex Reduction / Quality Delta はともに **unknown**。
同一要求・source・model/reasoning・評価入力・quality条件で対になる
Direct Codex / Codex + glm-worker のactual usageがない。
時間、fixture数、コミット数、GLM tokenをCodex削減率へ換算しない。
既存usage比較の採否は `220e453396a52f40efdfdd44c483c31a1910810b` を維持する。

最新の通常/raceは各6,204件のtest terminal、21件のtest skip、失敗0件。
package terminalを含めると各6,285件（pass 6,243、skip 42）。
raw test名集合の差はtelemetryのTask UUIDを含むsubtest名2組だけで、該当4recordはいずれもpass。
件数の一致を仕様coverageやQuality Deltaの同等性の証明にはしない。
#1594レビュー対応ではRoot復帰、要求改訂のsource binding、品質修復後のbinding、
Bundle選択・原要求回収に欠陥があり、`0bb9e2dc` で修正された。
通常/race、専用workflowintegration、Lint、install/smokeの既存成功証拠はあるが、
escaped defectがあった事実を消して品質改善率を算出しない。

## テスト時間

前回と今回はsource・test inventory・host負荷が異なるため、以下は観測比較であり因果的な改善率ではない。
package時間は並列実行されたpackageのterminal elapsedであり合算しない。

| 対象 | 前回通常 秒 | 今回通常 秒 | 前回race 秒 | 今回race 秒 |
| --- | ---: | ---: | ---: | ---: |
| full gate | 295.067 | 291.910 | 738.328 | 530.421 |
| app | 287.676 | 241.503 | 722.579 | 473.861 |
| controller | 131.052 | 155.403 | 176.855 | 199.501 |
| workflow | 45.761 | 42.219 | 59.729 | 60.082 |
| runner | 43.853 | 15.510 | 48.866 | 32.226 |
| state | 16.679 | 13.001 | 56.806 | 44.911 |
| parentactioncmd | 21.523 | 20.254 | 21.153 | 19.654 |

今回のrun identity:

- 通常 `38d16bc6c325d8d79cda66e9574a49ca`
- race `c289b96269c88e099e3d50942291208f`
- session `a0626d415c83947f3ec736893aff54d54be68ba4e4f9ea8faebd403a1c42212f`
- 正本は `~/.glm-worker/sessions/<session>/quality-gate-runs/<run>/{run.json,gate.log}`。
- 両runのHEADは上限コミット。index digestは `2b469d71c4f15bef8efbdc81289ea600a8c9b77119841eab2ed715b2d3c1b746`、worktree digestは `dd32e525624a8c8634747608f8ffb5938b730a1960e972c66a650a515194178e`。
- 前回runは通常 `f252d16b5d1ce2fbaaefccc738b38666`、race `e8693d9a3c1c17b1e31fc51bca1587ab`。詳細locatorは前回評価にある。

最新raceの遅い単体testは `TestUserTerminationProjectionRequiresCanonicalHandoffSelection` 35.92秒、
`TestObservationReplayFenceCapacityAndRecovery` 31.41秒、
`TestReboundCandidatePublishesOnceOnPulledSource` 27.11秒。
appは依然最大だがcontrollerも増えており、appだけの削減で全体改善を保証できない。

## CIの平準化

前回の既存run `41600207557` はquality 180秒、controller 164秒、test-1 51秒、
test-2 236秒、app 438秒でappがcritical pathだった。
current `.github/workflows/ci.yml` はapp通常6/race12 shardへ分割済み。
`tests/ci_app_shard.sh` は3つの重いcompletion testを明示配置し、残りを決定的に分配する。
他packageはharnesslint/workflowを別shardへ固定したうえで残りを分割する。
CIを新規照会していないためcurrent job/shard時間、queue・setup込みcritical path、
分割後の平準化の実効性はunknown。local full-suite elapsedをCI shard所要時間へ転用しない。
新しい重み付きschedulerや追加shardは、現時点では採用しない。

## 反復処理・停止と未Task化候補の採否

| 候補 | 判断 | 根拠・再評価条件 |
| --- | --- | --- |
| 開始projectionのState束縛 | 本Taskで修正 | 同じrepoでhandoffはmaterialize-parent-onlyを返すのにprepareが未admit。projectionだけがWorkflowConfigを通らず別namespaceのStateを読む。通常入口と同じ束縛へ揃え、State捏造や拒否の無効化をしない。回帰testは修正前fail、修正後pass。 |
| Publication graph探索 | 既存改善を維持 | #1593の同条件N64 benchmarkは2.226秒→31.974ms。実Publication約24分34秒→9分21秒は別Task間観測であり因果比較ではない。永続cache・全gate skipを追加しない。 |
| finalize→Publicationの結果再利用 | 追加実装No-Go | 既存completionはfresh validationを再利用する。`af5f0b78` の長時間runでGo/race/smoke以外の残時間を重複と断定できない。install、binding、promote、remote同期は別義務であり一括skipしない。 |
| Register/finalize全般の短縮 | 追加実装No-Go | 待ち時間だけから同一処理の二重実行を特定できない。既存stage ledger・traceが次の判断入力。今回新しい本番A/Bや汎用計測層を追加しない。 |
| child CLIのGORACE終了待ち短縮 | 今回は採用しない | 既存同source focused計測32.141秒→11.419秒、隔離race probeの警告・exit 66/97維持は確認済み。ただし全helperで終了前のgoroutine同期・出力回収が保証される証拠はなく、終了時race検出の同等性を断定できない。個別probeだけで全helperへ適用しない。 |
| Cleanup archive検証skip | No-Go維持 | 既存観測80回の合計37.5秒に対しCleanup約140秒。per-Store cacheは存在し、archive完全性を削る根拠にならない。 |
| session rotation閾値変更 | No-Go維持 | `ab0600d0` のmarkersは実際のclaim/bind比較cohortではない。新threadの同一条件usage・品質証拠なし。 |
| usage schema追加 | No-Go維持 | `220e4533` のsource input/output・cache/reasoning subset・epoch比較で表現可能。欠測を別推定値で埋めない。 |
| 親の過剰出力・無変化再入・手順変更 | 親の実行方法を是正 | raw全出力後の再projection、short wait、不要なsubagent、review手段の切替、Task分割はCodex消費を増やす。既存projection/dedup・long wait・same-session contractを使い、新しい類似guardを重複追加しない。今回も一部の出力を過大にしたため、遵守済みとは主張しない。 |
| reviewの独自呼出とusage欠落 | 再発させない | #1594対応の独自runnerは初回usageを保持せず後からcall accountingを回収した。初回tokenはunknown。今回の4行の接続修正はparent-onlyの親reviewと回帰testで扱い、評価のために追加model callを行わない。 |

GORACE案の詳細な既存観測は `/private/tmp/issue1567-race-helper-performance-instructions.md`、
Cleanupの採否は `/private/tmp/issue1567-cleanup-cost-assessment.json`。
一時artifactの欠損時に数値を再推定せず、該当比較をunknownへ戻す。

## importer Panicとparent-only Bundle

`15d5681a92161c559db3d05289ce8513bbf7d53b`（#1494）は
`qualityWiringPartialBindings` の外部export-data importer依存を除去した。
current `glm-worker/internal/harnesslint/quality_surface.go` の同関数は
Defs/Usesを局所型検査で取得する。固定Go wrapperを使う契約を維持し、旧export形式の互換層を作らない。
最新既存Lintの成功はあるが、将来の任意toolchain互換性まで保証しない。

parent-only Bundleは#1494でruntimeなしのlive attemptを扱い、その後#1594対応で
未公開の別Task sealによる明示選択失敗と、未公開Task原文のBundle欠落を修正した。
過去78回失敗（#1572 67回、#1568 11回、累計3,065,106ms）の全原因はstderr欠測でunknown。
#1594対応中の再現2件は同じ未公開seal境界で失敗し、修正後は対象3 Taskでexport成功。
#1593の `attempt/authority.md` は原要求7,465bytes、SHA256
`7371443b4322e6dd36d89e91c84dfd57277de25d5a1b1ee0ce95ecad2c3021ae` と一致した。
partial Bundleを完全coverageと呼ばない。

今回の最初のcompletionは、評価reportと次回Taskの変更を親がmutation記録へ反映せず拒否された。保存済みの正規admissionと実際の変更後snapshotを既存のRecordAdmittedMutationへ渡して記録する。拒否を成功へ書き換えず、Task identityとattemptを維持する。新しいproduct guardは追加しない。

今回も実際のparent-only attemptをmaterializeし、Amendmentを同じattemptへ同期した。
開始不一致の修正後、canonical prepare・typed operationを使用した。
通常Taskの成功や手動State修復をparent-only経路の成功証拠に代用しない。
最終source・validation・Bundle・Publicationの正本は今回のcontroller evidenceへ束縛する。

## 残Taskの優先順位と重複

残る全10件をPurpose/Contract/依存条件で比較した。

| Task | disposition |
| --- | --- |
| normal-workflow-organic-e2e-verification | NEXT先頭を維持。実modelと新規sessionでの通常経路の証拠であり、合成integrationや今回のCodex修正では代替できない。今回着手しない。 |
| post-105-codex-efficiency-reevaluation | 022直前を維持。今回を次の差分評価のbaselineにし、前回までの全探索を繰り返さない。 |
| 022-final-verification | 最終位置を維持。fresh release gateが責務であり、効率評価や通常経路live検証とは統合しない。 |
| user-requirement-ingress-binding | BLOCKED維持。取得可能なuser-turn identityの外部成立性を要求し、会話memoryの自己申告をmachine保証にしない。 |
| claude-cli-runtime-preflight-reevaluation | BLOCKED維持。実互換障害/help drift等のtriggerが今回の差分証拠にない。 |
| 101-live-sol-ab | BLOCKED維持。022依存と明示許可を保持。unknown指標を埋めるために勝手に本番A/Bを始めない。 |
| 103-compaction-threshold-change | BLOCKED維持。比較可能なusage/品質のfresh evidenceと許可なし。rotation案とは同じ変更ではない。 |
| 104-test-impact-selection | BLOCKED維持。test selectionとretirementの許可をGORACE待ち短縮へ読み替えず、coverageを減らさない。 |
| 106-review-call-reduction | BLOCKED維持。今回の親reviewを通常workflowの独立review削除へ一般化しない。 |
| 102-model-routing-redesign | BLOCKED維持。provider costとCodex実消費を区別し、sample不足のdowngradeをしない。 |

重複した新規Taskは作らない。post-105評価は今回と評価軸が重なるが、
通常経路live証拠後の最終差分を拾う役割を残す。
前回checkpoint以降の#1567 cohortだけで25件あり、最大5件の間隔は守られていなかった。
過去の間隔を遡って満たしたことにはしない。
今回は残るNEXTが通常経路検証→post-105評価→022であり、
次回評価まで1件、022直前にも評価がある。途中で5件を超える再計画または重大incidentが起きたら前倒しする。
