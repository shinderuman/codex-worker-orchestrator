# Task: bundle解析に不足する実行時証跡を記録する

## Original instruction

````text
そのタスクをつませたいが他のセッションでいま作業をしているので競合してしまう
とりあえずファイルを一時的なところにでも作っておいて他の作業が終わったらすぐ更新できるようにしておいて
優先順はさっきの**bundleにtask単位の分析用索引を付けるより先でいいと思っているが良いか**
````

## Amendments

none

## Resolved references

- 「そのタスク」は、2026-08-31のユーザー質問「他に解析するために収集すべきものとか出力すべきものとかあるの」に対して親Codexが提示した、次の追加収集・明確化候補を指す。

````text
追加収集の優先度が高いのは「実行時の条件」「処理をやり直した理由」「証跡がどこまで揃っているか」です。モデル・effort・usage・prompt・検証時間などは既に保存されているため、重複収集より不足部分を補う方が有効です。

| 追加・明確化したい情報 | 解析で分かること |
|---|---|
| **実行開始・再開時の環境情報**：実行binaryのrevision、CLI version、適用された設定・指示の識別情報 | 修正後のコードで実行したのか、古いbinary・設定で動いていたのか。現在のruntime設定出力はtimeoutの1項目だけです。 |
| **taskに紐づく工程境界**：GLM終端、親判断待ち、親採用、最終処理完了 | GLM処理・ユーザー待ち・親の確認作業を分離できる。現状の開始時刻やsession終端だけでは、task全体の終了境界が曖昧です。 |
| **再試行の因果関係**：何の再試行か、直前のcall/run ID、起動主体、理由 | 同じ処理の無駄な反復と、変更後に必要な再検証を区別できる。既存の理由欄・IDを優先利用し、取れない理由は不明にします。 |
| **検証対象自身の終了コード**と外側のshell/toolの終了コード | テストは失敗しているのに、`tail`や`exit 0`によってtool上は成功になる事例を検出できる。既存の固定検証入口から確実に取るのが先です。 |
| **収集範囲と整合性**：各証跡の最終event、収集時刻、実行途中・欠損・読取失敗、内容hash | 「失敗がなかった」のか「失敗する前までしか収集していない」のかを区別できる。 |

最後の点は実例があります。最新bundleは`task_status: active`・`in_flight_model_calls: 1`ですが、`evidence_status: complete`です。現状の`complete`をtask完了や解析材料の充足と取り違えないよう、収集成功と解析範囲の充足を分けて出力すると有用です。

全環境変数・全設定・無関係なsessionまで収集する必要はありません。
````

- 上記「最新bundle」は調査時の`e7aa8d95-31cd-48e7-ab47-450d9f91fc96.zip`（manifestの`created_at: 2026-08-30T21:43:54.457439Z`）。将来の最新bundleやtask状態を指すものではない。
- 検証終了コードが外側commandで隠れる実例はcommentlint task `314db004-148f-4b7d-b0e4-4a6dff90aa3a`のworker transcriptにある。`go test`の失敗出力に対してtool resultが`is_error: false`となるものを確認した。
- 既存の分析索引taskは`IMPLEMENTATION_TASKS/bundle-task-analysis-index.md`。工程別の時間・tokens・tool回数、高コスト処理の一覧、比較可能性などの集計・表示は索引側の責務として扱う。本taskは原始観測と収集範囲の不足を補う。
- 優先順位は本taskを分析索引taskより先に置く。実行中の別sessionや現在のACTIVEへ割り込まず、Planへの反映は他の作業が終了してから行う。実行順だけを理由にhard dependencyは設定しない。
- 今回はrepositoryとの競合を避けるため、一時ファイルで要求を保存するというユーザー明示指示を優先する。登録・実装・commit・pushは今回行わない。

## External feasibility

status: not-applicable

## Purpose

bundle解析で実行条件・工程境界・再試行の原因・検証成否・収集範囲を推測しなくて済むよう、既存証跡を再利用して不足する一次情報だけを残す。

## Contract

- 5つの観点ごとに既存のproducer・保存先・識別子・原本から導出可能な情報を確認し、収集済みの事実を別台帳へ重複記録しない。新規field・eventは既存証跡だけでは復元できない不足に限定する。
- 実行開始・再開時の実行binary、CLI version、適用設定・指示の識別情報をtask/callへ紐づける。bundle生成時点の設定を過去の実効設定として扱わず、観測時点・情報源を明示する。既存のprompt/hash・runtime build・session metadataを再利用し、設定は必要なallowlistだけを扱う。
- GLM終端、親判断待ち、親採用、最終処理完了について、既存lifecycle・親操作・rolloutから得られる工程境界をtaskと対応付ける。明示的に観測できない境界は不明とし、GLM終端や採用を親の全作業完了へ読み替えない。
- 再試行は既存のcall/run ID、phase、reason、origin、snapshotを優先利用し、再実行元・起動主体・理由を確認できるようにする。同一commandや時刻の近さだけで因果関係を確定しない。
- repositoryが管理する固定検証入口では、検証対象processの終了状態とwrapper/tool側の終了状態を区別して記録する。任意shell内の終了状態を復元できない場合は不明とし、外側の成功を検証成功としない。
- bundle内の証跡ごとに、観測できる収集時刻・最終event/収集境界・実行途中・欠損・読取失敗・収録内容の識別情報を示す。収集成功、task完了、解析対象区間の充足を区別し、同時刻snapshotでないものを整合済みと断定しない。
- 索引側が新旧の証跡を消費するときに、記録がない値をゼロや成功へ補完せず不明として扱えるようにする。原本の内容と参照可能性を維持する。
- 観測追加は既存実行経路へ接続し、task実行・acceptance・検証admission・権限・再試行方針を変更しない。bundle生成自体は原証跡とtask lifecycleに対してread-onlyを維持する。

## Must not

- API key・credential・全環境変数・全設定・無関係なsessionを追加収集しない。
- 不明な環境、終了時刻、再試行理由、内部終了コードを推測で埋めない。
- 観測のための追加model call、provider probe、検証suiteの再実行、周期polling、常駐daemonを追加しない。
- 汎用shell parser、modelによる原因分類器、独自state DB、第二のtask state machineを作らない。
- 原証跡を削除・上書き・切り詰めない。情報収集を理由にsandboxやGit authority guardを緩和しない。
- 待機再入、テスト反復抑制、finalize-checkのcwd、規則適用ラウンド、task diff欠落の実装修正を混在させない。
- 分析索引の集計・ランキング・表示を本taskへ取り込まない。

## Acceptance criteria

- 5つの観点について、既存証跡の再利用箇所・不足を補った箇所・観測不能として残る境界を原本参照付きで確認できる。
- 実行途中に設定やbinaryが変わる例で、開始/再開時の条件とbundle生成時の条件を混同しない。
- GLM終端後も親処理が続く例、判断待ち、再開・再試行の例で、工程と理由をtask/call/runへ帰属させ、不明部分を明示できる。
- 検証対象が失敗して外側commandが成功する例を、検証成功として集計させない。取得不能な内部終了状態は不明として残す。
- 実行中bundle、正常完了bundle、欠損・途中までの証跡で、収集成功と区間充足の違いを機械的に読み取れる。
- 新規記録とbundle収録がproduction経路につながることを代表的な検証で確認し、秘密情報の非収集、原本保持、追加model call不要、既存lifecycleと権限境界の維持を確認する。

## Historical invariants

- task/call/session identity、exact-snapshot validation、missing/unattributedの区別、親Codexのsemantic authorityを維持する。
- 観測結果は診断用とし、成功判定やtask要求のauthorityを置き換えない。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。別sessionの作業終了後のユーザー指示に従いrepositoryへ登録した。Original instructionとResolved referencesにある一時保存限定は当時の実行境界であり、登録後も継続する制約ではない。実装開始時に既存証跡と不足を再確認し、既に追加済みの観測を二重実装しない。
