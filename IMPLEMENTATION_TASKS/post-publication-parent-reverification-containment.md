# Task: Publication正常終了後の親Codexによる根拠のない再検証・探索を抑制する

## Original instruction

2026-10-28 ユーザー指示（原文）:

````text
Codexは被害妄想に取り憑かれているからどれだけ「正常に終了した」というレスポンスを返すようにしても自分でクソみたいな情報を探してしまうと思う
つまり実装してもいたちごっこになりどんどん収集する情報が増えていく
Findingとして起票していいが優先度は[`repository-review-artifact-cleanup.md`](https://github.com/shinderuman/codex-worker-orchestrator/blob/main/IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md)の前とかでいい
今後のAuditでも多分似たようなクソ作業をCodexは繰り返すので作業までに情報を蓄積していく
とりあえずPRを作っておいてくれ
````

## Amendments

none

## Resolved references

- 「今回」「Finding」: #1410 Dogfood Audit、Bundle `0feff520-43b4-4d19-b168-203254fe5c07.zip` のPublication terminal成功後の親Codex追加調査。Issue #1411はWeb GPT管理・後続Audit証拠を収集する場所であり、実装要求正本はこのTask。GitHub Issueのqueue/label/management stateをCodex/GLMのTask authorityとして渡さない。
- 監査の一次証拠: Publication terminalは `status=completed`、final/remote OID一致、`stop_boundary=next-task-not-started` を返した。その後Codexは別途`git ls-remote`、HEAD/clean/1-commit、installed runtime、Task退役、validation証拠を再確認し、SSH DNS失敗と再試行、任意の完了証跡JSON生成・一時tool片付けを行った。Publication後に新たな実装・テスト・commit・次Task開始は認められない。
- 観測値: terminal 2026-10-28T06:40:48Z →最終報告06:45:51Z、親モデル応答4件、tool呼出3件、total tokens 1,782,770（cached input 1,772,288）。cached inputは課金削減可能量そのものではなく、後続観測で原因別に帰属する。
- 現行実装参照: `glm-worker/internal/parentactioncmd/completion_workflow.go`、`terminal_envelope.go`、`glm-worker/internal/controller/publication_publish.go`。terminal返却後のhandoffにはNEXTに対する`continue-now`が並び、terminal `next-task-not-started` と混同し得る。ただし今回のBundleでNEXTを開始した証拠はない。
- 既存 `publication-sequence-roundtrip-evaluation.md` はPublicationの定型stage間の往復と意味判断の分離を評価する。**本Taskは正常終了後の追加再検証の発火条件・抑制境界**が中心であり、既存Taskのstage最適化を再実装しない。`canonical-publication-source-continuity.md` のremote/source integrityと既存publishのremote観測を正規ownerとして保持する。
- 後続Dogfood Auditでも同種の動作が発生する見込みというユーザー見立ては、発生を確定した事実に置換しない。追加証拠は各Auditのraw BundleとAudit Issueを正とし、管理用#1411へそのexact locator/差分を蓄積する。本Taskへ進捗日記やモデル使用量の最新値を複製しない。

## Purpose

Publicationが必要な検証・状態遷移・remote同期を成功裏に完了した後、親Codexが根拠なく新しい「念のため」確認や情報探索を開始してモデルターン・ツール呼出・ネットワーク依存・完了遅延を増やすことを防ぐ。**返却フィールドを増やすたびに別の確認を探す、いたちごっこを解消すること**を目的とし、ユーザー指示や真正の不一致に対する必要な追加調査は維持する。

## External feasibility

status: not-applicable

## Contract

- 着手時点の#1410および後続Auditのraw一次証拠を対象期間・exact locator付きで照合し、terminal成功後の全親model turn/tool callを「既存証拠の再照会」「要求上必要な未証明条件」「真正の新情報/異常」「ユーザー指定行為」「単なる最終報告」へ分類する。実測の有無を明示し、未観測の将来発生を再発実績と扱わない。
- 本来のPublication / finalizationが既に検証・保証したことと、terminalが実際に返却した証拠、親が後追いで収集した証拠、親の意味判断が残ることを区別する。必要な欠落があれば正規command/ownerで検証・記録できることを評価するが、**成功レスポンスへ検証項目を無限追加することを解決策としない**。
- 成功したsource-bound terminal結果と既存の機械証拠が有効で、ユーザー追加要求・未履行acceptance・新たな不一致・実行失敗・有効性を失ったsnapshot等の具体的理由がない場合、親Codexの任意のremote再取得、同じGate再読、status/commit探索、手書きの完了証跡生成へ再入しない境界を検討し、現在の適切なownerへ接続する。注意文やモデルの自己申告だけではなく、現行tool/handoff/continuation/stop boundaryの**機械的な再入抑制・根拠要求と検証可能な適用範囲**を設計・実証する。
- 「再検証したくなった」こと自体を根拠にしない。追加確認を許す場合は対象のexact invariant、未証明/失効した証拠、現在source/identity、必要な追加アクションをboundedに明示し、成功済み処理や同じraw dataの再投影を繰り返さない。親のmeaningful semantic decisionと機械的結果確認の境界を崩さない。
- `completed`と後続Taskに対する`continue-now`が同じterminal envelopeにある場合、**終了したTaskに追加操作を要求しているのか、別Taskの開始可能性なのか**を明確にし、成功したTaskの終端に不要なCodex再入を誘導しない。新Taskを勝手に開始しない。
- 出力を増大させる改善は費用対効果を比較し、親が新たな確認項目を求める余地を広げない。現在のsourceでPublication後の必要なcleanupがterminalの正規責務なら、そのcleanupと証拠保存を親の追加自由手順へ押し出さない。過去の手動検証JSONを長期正本にしない。
- 実装の前にbounded evidenceから対策を採否判断する。安全な機械境界が成立しない場合は未知/No-Goを根拠付きで返し、警告文追加や無意味な確認項目増殖で見かけ上の修正完了にしない。成功・失敗・remote不明・Task未完了・user追加指示などを区別する。
- 変更採用時は観測された後処理に対するmodel turn、tool call、input/cache/output token、wall、失敗・再試行回数を比較可能なcohortで測定し、Codex ReductionとQuality Deltaの両方で評価する。結果や限界はAudit/Bundleへ残し、Task fileへ可変計測snapshotを追記しない。

## Must not

- 親に「以後の確認をするな」と自然言語で注意するだけで対策完了にしない。
- terminal successの証拠なしに検証を黙って省略する、source不一致/不完全なremote結果を正常と偽装する、ユーザーが要求した確認を禁止する、真のfailureを黙殺することをしない。
- Publicationのremote write permission、Git/Task/source identity、独立Review、required CI/validation、1 Task 1 Commit、正規Recoveryを弱めない。
- 全terminal responseへ新たな検証フィールド/詳細証跡を無制限追加しない。新daemon、第二state DB、旧schema互換fallback、都度のPython/shell wrapper、追加AI reviewerを対策の前提にしない。
- Publication**前**のstage連結最適化と、本Taskの**Publication後**の親再入抑制を重複実装しない。
- ユーザーが許可していないNEXT開始、現ACTIVEの割込み、次Taskのモデル呼出を行わない。

## Acceptance criteria

1. #1410のPublication完了後にCodexがremote/OID/1-commit/clean/runtime/validationを再確認し、DNS失敗→再試行、独自JSON作成、追加の4 model responses / 3 tool callsに至ったsequenceを原典から復元して各確認の必要性・根拠・コストを分類する。後続Auditで見つかった同種/別種の再検証も同じ基準で追加評価する。
2. 正規terminal成功・source一致・既存証拠有効で追加要求なしのケースでは、同じ証拠の無根拠な再取得・完了後追加判定の反復を抑制できることを、**実際のparent→machine interface**またはproduction ownerを駆動する検証で示す。単に「確認不要」とterminalへ出す実装はPASSとしない。
3. 未履行義務、terminal失敗、remote未確定/不一致、source失効、新user instruction、実際に異常を検出したケースでは、必要なbounded確認または正規recoverを妨げず、なぜ追加確認が合法かを追跡できる。
4. terminal結果の`next-task-not-started`と後続Taskの状態を取り違えて余計な親行動や無許可NEXT開始が起こらない。既存publish遠隔観測と結果の再利用が確実で、同じremoteへ重複問い合わせしない正常経路を確認する。
5. 実測比較で不要な親turn/tool call/時間が減ったか、Quality Deltaの悪化がないかを評価する。削減量が計測できなければ未証明と明記し、完了宣言に使わない。後続の通常Dogfoodでlive effectivenessを観測するまで「実効性検証済み」と扱わない。
6. 既存owner再利用・範囲限定のdiff・関連test/lint/Quality Gate・独立Review・正規Publicationが成立する。今後の追加Audit事例を取り込めることと、control自体が新たな無限確認の起点にならないことを確認する。

## Historical invariants

- 正規Task要求はGitのOriginal instruction/Amendments/Resolved referencesを正とする。#1411や#1410は管理/一次証拠locatorであり、Codex/GLMへの別authorityではない。
- source identity、remote Git safety、ユーザーpermission、モデルsemantic判断、独立Review、required validation、機械State、通常1 Task 1 Commitを維持する。
- 通常の運用・監査結果はBundle/telemetry/Auditで回収する。Issue #1411に後続Auditへの参照を蓄積するが、Task fileへchronologyを追加しない。

## Dependencies

none
