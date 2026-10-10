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

## Resolved references

- Bundle A: runtime Task UUID `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`。固定ZIP `~/.glm-worker/exports/f6189a2ef4b9698296980938dcb3f04542eadb0638397e403f7925130b92cfc4/d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4.zip`、実装commit `8b7556b1b47cdc85fd101b13c83d2c0e60ecc66f`、元要求は同commitの`IMPLEMENTATION_TASKS/dogfood-bundle-audit-restoration.md`。Task関連のcompletion-exception receiptを含め、例外と通常成功を区別する。
- Bundle B: `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`を次の新セッションで完了したTaskのruntime UUIDをcanonical Task evidenceから解決する。現在Taskの例外を正常経路へ持ち越さない。
- Bundle C: B完了後に実施する通常開発Task。既存優先順位を維持して`IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`を1件の対象にする。B完了前の旧CLI誤起動sessionはCとして採用しない。
- A→B→C→本再監査の順序を守る。Task定義とPlan登録は実行開始を意味しない。

## Purpose

例外を含む長期Task、コマンド修復Task、例外なしの通常開発Taskの三つを比較し、Task全体のDogfood Auditと実装の単純さが成立するかを確認する。実装修正を必須成果にしない。

## Contract

- 既存`glm-parent-action export-bundle`を用い、Task IDごとの最新ZIP一つを検証する。Attempt間とseal後を含む取得可能な親Codex・Guardian・GLM生ログを維持し、収集側でAudit対象を過剰に選別しない。
- instruction→execution→failure/retry→review→validation→Git/publication→completionを各Taskの一次証拠から再構成する。ファイル存在や親のPASS認定だけでAudit成立とは扱わない。
- manifestのSHA-256/bytes、ZIP内パス衝突、Task/Attempt/session関連、欠損の正直な表示と封印証拠の不変性を確認する。
- 生成時間/容量/構成を比較するが、長期化や複数Attemptの違いを考慮する。異なるパスの同内容、多少の余分なログは許容する。
- 実質的な退行を確定したときだけ原因と影響をSolへ提示し、既存owner内の必要な改善をGLMに委譲する。問題がなければFindingなし・実装修正なしで正常終了する。

## Must not

- BとCの完了前に本Taskを開始しない。現在のBundle復旧完了直後に再監査を開始しない。
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

- `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`
- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`
