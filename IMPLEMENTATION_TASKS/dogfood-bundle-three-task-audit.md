# Task: 三つの通常・復旧BundleによるDogfood Auditの再確認

## Original instruction

以下はユーザーの追加指示の原文である。

````text
## 4. 三つのBundleを比較する後続Taskを準備する

Bundle復旧後の改善確認を、別Taskとして予約しろ。

ただし、**今回すぐに再監査するな。**

次の順序を守ること。

1. 今回のBundle復旧Taskを完遂する。
2. `normal-workflow-snapshot-stop-regression.md`を新セッションで実施する。
3. その後、Bundle以外の通常開発Taskを少なくとも1件完遂する。
4. その後でBundle再監査Taskを実施する。

Planの既存NEXTの優先順位を不必要に壊さず、この順序を満たす位置に再監査Taskを登録すること。

### 再監査Taskの目的

以下の三つのBundleを比較する。

**Bundle A：今回のBundle復旧Task**

- 長期化した作業
- 複数Attempt
- 親Codex・Guardianの長大なログ
- State不整合と例外操作
- 一時的な完了処理

**Bundle B：次のコマンド不具合修正Task**

- コマンド修正前後の動作
- 品質ゲートと正規Validationの関連付け
- Handoff、Review、Publication、Task終了
- stop/resumeとsnapshot整合性

**Bundle C：その後の通常開発Task**

- 例外操作を用いない通常運用
- 一般的な長さ・構造のBundle
- 実際のDogfood Auditに必要な証拠の収録

三つのBundleについて、Task全体の一次証拠からAuditできることを検証する。

ファイルの存在だけではなく、instruction、execution、failure、retry、review、validation、Git変更、completionを再構築できることを確認する。

また、生成時間や容量についても比較するが、単純なサイズ削減を目的にしない。

**ファイルが重複しているという理由だけで削除を要求しないこと。**

多少の余分な証拠は許容する。重要なのは監査可能性と実装の単純さである。

### 再監査Taskの終了条件

**実質的な問題がなければ、コードを一切変更せずにTaskを終了してよい。**

これは改善実装を必須とするTaskではない。

- 問題があれば、原因と影響を確認して必要な改善を行う。
- 問題がなければ、Findingなし・実装修正なしで正常終了する。
- 容量、命名、コード行数、見た目だけを理由に不要な修正を作らない。

再監査Taskには、この判断基準も最初から明記すること。

````

## Amendments

### 2026-10-13 Plan・Task整理と実行境界（ユーザー原文の本Task該当節）

````text
既存の実行順序は次のとおり維持する。

ACTIVE:

- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`

NEXT先頭:

- `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md`
- `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`

その他のNEXTは、既存の相対順序を維持する。

旧Task `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` は、ユーザー判断によって実装成果を確定し、未達事項をWorktree見直しTaskへ引き継いでいる。

旧TaskはACTIVEへ復帰させず、BLOCKEDに保持する。通常のsemantic acceptance、Publication、Retireが成功したと扱わない。

ACTIVEへのCLI Task登録はスケジュールの確定だけであり、このセッションでのTask activation、GLM dispatch、Runtime再開を意味しない。

Worktree見直しTaskに、旧Taskからの未達事項が引き継がれていることを確認する。

特にgeneration 713で停止したcleanup後のRuntime Snapshot／Handoff／materialize、および未達のPublication・Retireが対象に含まれていることを確認する。

Task本文に残る旧ACTIVE前提の記述、現在のScheduleと矛盾するderived contractは確認する。必要なら正規のAmendment／derived contractの規則に従い整合させる。

ただし、Original instructionや既存Amendmentsの原文を削除・改変しない。

このセッションでは実Taskを開始しない。
````

今回の明示順序と未達事項移管は、旧Taskの正常完了後にCLIを実施する従来の順序前提を上書きする。旧Taskを成功dependencyへ変換せず、再監査で必要な未達Workflowの検証は既存Worktree Taskの成果を参照する。A/B/Cの監査対象と実在証拠の保全は維持する。

## Resolved references

- Bundle A: runtime Task UUID `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`。固定ZIP `~/.glm-worker/exports/f6189a2ef4b9698296980938dcb3f04542eadb0638397e403f7925130b92cfc4/d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4.zip`、実装commit `8b7556b1b47cdc85fd101b13c83d2c0e60ecc66f`、元要求は同commitの`IMPLEMENTATION_TASKS/dogfood-bundle-audit-restoration.md`。Task関連のcompletion-exception receiptを含め、例外と通常成功を区別する。
- Bundle B: 旧Taskの要求・証拠参照はGit履歴 `c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` を正とし、`git show c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` で取得する。runtime Task UUIDは保全済み `quiescent-cleanup-handoff-stop-713/stop-summary.json` の `runtime_task_id` にある `24989cdb-8db0-4f9f-8e65-45516dc66380` と照合する。ユーザー判断による実装成果確定と、未達の通常semantic acceptance・Publication・Retireを区別して監査する。引継ぎ先 `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md` の正規Workflow検証は関連証拠として照合し、旧Taskの正常完了へ付け替えない。
- Bundle C: `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md` を通常開発Taskの対象として維持する。旧CLI誤起動sessionはCとして採用せず、正規実行のcanonical Task evidenceを使う。
- 既存原文の旧Task参照は上記Git履歴を指す。旧Taskの削除・スケジュール除外を成功済みdependencyと扱わない。実行順序はPlanだけを正とする。旧Taskの成功完了をCLI開始の前提とせず、本再監査に必要なCLIとWorktreeの成果を待つ。Task定義とPlan登録は実行開始を意味しない。

## Purpose

例外を含む長期Task、コマンド修復Task、例外なしの通常開発Taskの三つを比較し、Task全体のDogfood Auditと実装の単純さが成立するかを確認する。実装修正を必須成果にしない。

## Contract

- 既存`glm-parent-action export-bundle`を用い、Task IDごとの最新ZIP一つを検証する。Attempt間とseal後を含む取得可能な親Codex・Guardian・GLM生ログを維持し、収集側でAudit対象を過剰に選別しない。
- instruction→execution→failure/retry→review→validation→Git/publication→completionを各Taskの一次証拠から再構成する。ファイル存在や親のPASS認定だけでAudit成立とは扱わない。
- manifestのSHA-256/bytes、ZIP内パス衝突、Task/Attempt/session関連、欠損の正直な表示と封印証拠の不変性を確認する。
- 生成時間/容量/構成を比較するが、長期化や複数Attemptの違いを考慮する。異なるパスの同内容、多少の余分なログは許容する。
- 実質的な退行を確定したときだけ原因と影響をSolへ提示し、既存owner内の必要な改善をGLMに委譲する。問題がなければFindingなし・実装修正なしで正常終了する。

## Must not

- CLI Taskと引継ぎ先Worktree Taskの必要成果が揃う前に本Taskを開始しない。旧Taskのユーザー判断による区切りを正常完了として、再監査の前提を満たしたと扱わない。
- 容量、命名、コード行数、重複だけを根拠に改善を創作しない。重複排除機構、別の履歴/証拠管理、旧CLI alias、後方互換fallbackを追加しない。
- 復元不能な過去ログを捏造しない。収集境界をAttemptへ縮めない。Task UUID体系/固定atomic ZIPを変更しない。
- controllerのcanonical evidence、digest検証、path traversal/rebind fail-closed、read-only export境界を弱めない。今回の例外操作を通常運用として採用しない。

## Acceptance criteria

1. A/B/Cのruntime Task ID、Git結果、関係するAttempt/session、関連付け根拠を報告できる。
2. 三つともTask全体の指示・実行・失敗・retry・review・validation・Git・completionを存在する一次証拠からAuditできる。欠損は理由と実影響を明示する。
3. ZIP衝突とmanifest不一致がなく、生ログが不必要に切断されていない。取得のread-only性を維持する。
4. 比較結果と生成時間/容量を根拠付きで報告し、実問題がなければコード変更なしで終了する。必要な実装修正を行った場合は通常の独立review/品質検証/実ZIP確認を完了する。

## Historical invariants

- single canonical controller evidence authority、Parent/operator read-only export、Task UUID単位の固定atomic ZIPを維持する。
- Auditの解釈と収集を分離し、親Codex自身の不正規操作も一次ログから監査できるようにする。
- Codex ReductionとSol品質判断を維持し、必要なrepository調査・実装・検証はGLMへ委譲する。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md`

- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`
