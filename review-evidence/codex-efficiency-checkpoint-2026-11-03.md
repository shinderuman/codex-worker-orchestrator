# Codex効率の中間評価（2026-11-03）

## 比較範囲と限界

前回は `review-evidence/codex-efficiency-checkpoint-2026-10-29.md`、今回のproduction基点は `e3dcec1525adbee32e33078928510c94dcdbb404`。差分は #1444 の22 Task取り込みと #1478 の7 Draft PRの引継ぎ準備に限定する。同一要求・source・model/reasoning・開始終了境界を揃えたDirect Codex対Codex+GLMのpairと、今回区間に帰属する親usageは未取得であり、Codex Reduction / Quality Deltaの数値はunknown。追加A/B、全履歴再走査、追加モデル呼出しは行っていない。

## 検証済み事実と品質

#1444の22 TaskはGit上で22個別commitとして取り込まれ、`e3dcec15`までremoteへ反映された。終端sourceのrepository lintは `/private/tmp/issue1444-pr1456-terminal-metadata-lint.json` のpass・fixed 0・violations 0、本配置smokeは `/private/tmp/issue1444-pr1456-final-install-smoke.json` のpass。Go/raceは旧controllerのparent-only候補に対して正規finalize-checkを実行し、終端Go sourceと候補の一致を確認した。これはDirectとの品質同等性や最初から全TaskでLintしたことを意味しない。後段のLint不足と修正は品質上のincidentとして扱い、全source Lintの成功を過去各境界の成功へ付け替えない。

旧controllerを再初期化した際のobserved prefix不足と、未公開local履歴とremote基点の不一致により、正規Publicationまで到達できずState保全・手動同期を要した。最終22 commitのPushはユーザーが実施した。親の初期State設定不足と、通常の1件ずつの公開を前提とするownerへ未公開batchを接続した問題を区別する。成功した通常workflowとして報告しない。今回のparent-only開始はmodelなしの実attempt/leaseを既存Controller ownerで生成し、Git管理された実行指定を開始前に固定する。

## appの責務と既存Taskへの帰属

#1451は集計をbundleanalysisへ移す変更として取り込み済み。appの行数だけで効果を判定せず、集計の意味を持つownerとCLI入力・出力の責務移管を採用根拠とする。Controller/runtimeの遷移調停は現mainではappに残り、#1483は未採用候補である。workflowへファイルを移しただけで完了とせず、部分commit後の冪等復旧・binding・checkpoint・typed CLI/handoff一致を確認する。

正規操作の外部審査と親往復はguardian-facade-review-effectiveness、無変化の親model再入はparent-inflight-wait-admission、usage比較不能はcodex-usage-comparability-evaluation、Publicationの基点/往復はpublication-adopt-mutable-projection-admissionとpublication-sequence-roundtrip-evaluationが既に扱う。今回の同種事象を別の復旧frameworkや重複Taskへ増殖させない。7 PRへの先行採否は各PRのCodexコメントを正とし、未対応条件を完成扱いしない。

## 優先順位と採否

ユーザーが開始を指示した #1478 の7件はIssue記載順に取り込み、採否・追加修正・検証・MetadataはCodexが所有する。Web GPTの完成表示やCI successだけでは親の最終採用にしない。GLMは7 Taskで必要に応じて使用できる。本checkpoint固有の追加モデル禁止を他Taskへ一般化しない。後方互換、旧schema fallback、flowを通すためのGuard緩和は採用しない。

全残TaskのPurpose/Acceptanceと前回の担当候補評価を照合し、評価・実ログ・外部審査・有機的E2EのTaskはCodexに残す。BLOCKEDの許可条件とhard dependencyは解除しない。正規scheduleはPlanに集約し、本書へ一覧を複製しない。5件の取り込み後、残る2件の前に次回中間checkpointを配置し、post-105は022直前を維持する。

親Codexは比較不能をunknownに保持した本評価、責務改善と行数削減の区別、既存Taskへの帰属、Plan優先順と次回checkpointの整合を採用する。production実装や新しい本番A/Bは本評価に含めない。
