# Task: Observation実行identityの再実行拒否寿命

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
結果履歴の保持上限で同一decision roundの再実行拒否を失わない。
in-flight→indeterminate/pass/fail後も、同じTask/decision round/operation/params identityの再admissionを拒否する。結果表示の16件retentionと拒否identityの寿命を分離する。
既存state/lifecycle ownerへ収束し、第二DB・daemon・無期限に増える履歴を追加しない。round/task終端での安全な解放条件を明示する。
永続状態の意味や拒否条件の変更案は実装前にSol判断へ戻し、旧schema互換を設計理由にしない。
````

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1482#issuecomment-6588300667

````text
実装前のCodex判断です。PR本文の4点について、次の方針で実装を進めてください。

基点は `e3dcec1525adbee32e33078928510c94dcdbb404`、確認対象はPR head `a8a14a614a0d95e8dc58a5221194626f213dff62` です。[現行のHas/Beginとtrim](https://github.com/shinderuman/codex-worker-orchestrator/blob/e3dcec1525adbee32e33078928510c94dcdbb404/glm-worker/internal/state/observation_execution.go#L137)を確認し、表示履歴の削除によって同一roundの拒否identityも失われるという原因を採用します。追加された回帰テストの狙いも妥当です。ただし、ここではテストを実行しておらず、実装・検証のPASSやMerge承認ではありません。

### 1. 永続状態と保存の単位

既存 `StateStore` と `observation-executions` の所有範囲で、**単一のcurrent-format document** に次を持たせる方針を採用します。

- 所属するTask IDと、fenceが対象とするcanonical decision round。
- 再実行拒否用のコンパクトなidentity集合。対応するexecution IDも保持し、同round内のexecution ID重複を履歴のtrimで見失わないこと。
- 詳細record。terminalは直近16件、未解決in-flightはtrim対象外。

fenceと詳細recordを別ファイルへ分けて二重書込みする案は採用しません。`Begin`では、fence予約とin-flight recordを**同じatomic writeで永続化してから**subprocessを起動してください。既存repository lockによる直列化を維持し、Hasでの事前確認だけに依存せず、Begin自身でも再確認します。Complete、Recover、ResolveIndeterminate、Appendが残る場合のAppendも同じownerで整合させ、別の書込み入口からfenceを迂回できないようにしてください。

新形式だけを受理し、旧配列の読取り・migration・alias・推定fallbackは追加しません。壊れた状態や必要なstateの欠損を空集合へ読み替えることも禁止です。新Task用の空documentは既存のTask初期化/rollback境界へ組み込み、稼働中の旧状態を自動変換して継続させないでください。

### 2. 拒否identityと遅延結果

再実行拒否の意味は現在の **Task ID + canonical decision round + operation + params digest** の完全一致を維持します。execution IDの変更では再実行を許可しません。in-flight / pass / fail / indeterminateをすべて拒否対象とし、同じroundの異なるparams、および安全に切り替わった次roundの同じparamsは許可します。

roundは要求側の自己申告ではなく、現在の `ObservationExecutionRound()` / lifecycle admissionへ照合してください。過去・未来roundや別Taskの要求を、fenceがないという理由でadmitしてはいけません。

completionは元のexecution ID・Task・round・operation・paramsとdurable in-flightへの一致が必要です。[dispatch中にはrepository lockを解放する現行経路](https://github.com/shinderuman/codex-worker-orchestrator/blob/e3dcec1525adbee32e33078928510c94dcdbb404/glm-worker/internal/parentactioncmd/observation_execute.go#L110)も考慮してください。境界変更後の結果を新roundの成果へadoptせず、既存のindeterminate処置を維持します。回収済みterminalを遅延したpass/failで上書きしたり、消えたin-flightを再生成して受け入れたりしないでください。

### 3. 解放条件

**結果表示のtrim、pending-decisionの解除、時刻の経過だけではfenceを解放しません。**

同じTaskでcanonical roundの前進を確認し、旧roundのin-flightがすべて正規completionまたは既存recoveryによるdurable indeterminateへ解消済みの場合に限り、次roundの最初のBeginを保存するatomic writeでfence集合を切り替えます。旧in-flightが未解決なら、次roundの新規observation admissionをfail closedにし、旧記録を保持してください。Hasはこの状態を判定できても、読取りだけでGCを書き込まない構成にします。

Task終端では未解決実行の処置と旧Task authorityの失効を確認してから解放し、新Taskの正規初期化で空の状態へ切り替えます。初期化/round切替の途中失敗・再起動でも、拒否証拠だけ先に消える状態を作らないこと。Task/roundの前進後に旧要求を拒否する境界が、GCの安全性の前提です。

### 4. 上限

今回の固定上限は **1 Task・1 active roundあたり1024個の異なる実行identity** とします。これは性能上の最適値を測定したという意味ではなく、無期限成長を防ぐ安全上限です。terminal/in-flightを合わせて数え、1025個目の新identityはsubprocess起動前に明示的な容量エラーで拒否します。古いfenceの押出し、暗黙のround更新、上限回避用の自動retryは行いません。

上限到達後も、既存実行のComplete/Recover/Indeterminate処理と重複照合は継続可能であること。コンパクトなfenceへpayload・詳細error・artifact一覧を重複保存せず、不要な設定項目や第二DB・daemonは追加しません。

### 実装後に確認すること

- 今回の17件超のpass/fail/indeterminate回帰と、新execution IDによるreplay拒否。
- Has→Begin間の競合、永続予約前/後の失敗、再起動後の拒否維持。
- 同round別params、次round同params、stale Task/round、旧in-flightが残るround切替。
- 遅延completion、indeterminate確定後の結果到着、破損/旧形式/必要state欠損。
- 上限ちょうどと上限超過、上限到達後の既存実行の完了・回収。
- 正規のparent action入口まで接続した正負例、repository-owned test/lintと独立Review。

以上を本PRで実装してください。Task/Planは親Codex専有なので、Web GPT側で完了Task削除やPlan変更を先行させず、必要な更新候補と根拠を返してください。全Acceptanceの採否とMetadata同期はCodex側で行います。現在のcheckpoint先頭という順序は変更しません。
````

### 2026-11-03 Task終了時の回収接続

- Task終了後にdispatchが残したin-flightを、次Task開始が永久に拒否したままにしない。正規Task初期化のrollback境界内で、既存のdurable recovery horizonを過ぎた実行を既存ownerによりindeterminateへ回収し、未解決が残る間は新Taskを拒否する。期限前の見切り解放や新しい復旧DB・daemonは追加しない。
- current documentの`round`欠落/nullは、空fenceの初期値0と推定せず形式エラーとする。

## Resolved references

- PR #1343 / comment 6110431434: RecoverStaleObservationExecutionsがindeterminateへ変更した後、trimObservationExecutionsはin-flight以外を16件へ制限する。Has/Beginのduplicate判定は残存recordだけなので、同一decision roundで17件以上のterminal追加後に同じoperation/paramsを再受理し得る。結果履歴とreplay拒否identityの寿命を同一視しない。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/state/observation_execution.go:RecoverStaleObservationExecutions`
- `glm-worker/internal/state/observation_execution.go:trimObservationExecutions`
- `glm-worker/internal/state/observation_execution.go:HasObservationExecution`
- `glm-worker/internal/state/observation_execution.go:BeginObservationExecution`

## Purpose

結果履歴の保持上限で同一decision roundの再実行拒否を失わない。

## External feasibility

status: not-applicable

## Contract

- in-flight→indeterminate/pass/fail後も、同じTask/decision round/operation/params identityの再admissionを拒否する。結果表示の16件retentionと拒否identityの寿命を分離する。
- 既存state/lifecycle ownerへ収束し、第二DB・daemon・無期限に増える履歴を追加しない。round/task終端での安全な解放条件を明示する。
- 永続状態の意味や拒否条件の変更案は実装前にSol判断へ戻し、旧schema互換を設計理由にしない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- 同じroundで17件以上のterminal記録を追加しても、古いindeterminateを含む同一identityが再実行されない。
- 別params/別roundの正当なadmissionと、遅延completion・中断復旧の拒否境界が維持される。
- 保持量の解放条件とreplay防止の証明が両立する。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
