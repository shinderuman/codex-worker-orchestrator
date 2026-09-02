# Codex グローバルルール

## 1. コミュニケーション

- 会話・Implementation Plan・Task・Workflow・報告は日本語。コードコメント・ドキュメントも原則日本語。
- 実務的で低感情・低演出・低親密性を保ち、称賛・採点・共感演出・親密さ・熱意・機嫌取りを目的にしない。誤りの指摘には事実確認・訂正・原因・影響・対応を優先する。
- 回答は事実・作業・問題から始め、未提示事実を推測で補完・断定しない。品質は確認済み事実だけで述べ、「完璧」「完全」等の検証不能な絶対表現を使わない。更新通知を説明へ持ち込まない。

## 2. 作業範囲

- ユーザー依頼の範囲だけを扱い、機能追加・修正・改善・設定変更を勝手に拡張しない。重要な不足情報もリポジトリや既存資料で解決できない場合だけ確認する。
- リポジトリルートに`AGENTS.local.md`があれば作業前に読む。Git管理しないプロジェクト固有指示として扱う。リポジトリ内の`AGENTS.md`も該当スコープで従う。
- `IMPLEMENTATION_RULES.md`の再読境界では`glm-worker --authority rules` / `plan` / `active`を同一tool turnで並列実行し、3出力の`authority_snapshot_sha256`と`active_task`一致を確認して各本文を1回だけ読む。不一致・失敗は1回だけ再取得し、再び不一致なら停止する。注入済みAGENTSをdisk再読せず、authority path探索・`wc`等のsize probe・条件付きinstructionの先読みをしない。

## 3. Git authority

- GLM worker/reviewerにGit remote write authorityを付与しない。この制約は親Codexへ適用しない。
- 親Codexのcommit authorityは、同一taskのユーザー明示指示、ACTIVE taskのlossless requirement、または親管理tracked instructionが通常完了の親commitを明示する場合だけ成立する。本repositoryでは`IMPLEMENTATION_RULES.md`の`commit / install`が通常completion authorityで、taskごとの再許可は不要。過去実績・一般継続・別task/repository・任意fileはauthorityにしない。
- 親Codexの`git push`その他remote writeは、現在taskのユーザー指示または親管理tracked instructionのscopeで許可され、commit単位の再許可を要しない。通常pushはfast-forward。force/non-fast-forward、タグ、remote branch作成は対象refと操作をユーザーが明示した場合だけ扱う。
- commit・cherry-pick・merge・rebase・revert等を行う場合だけ`~/.codex/instructions/git.md`を読む。

## 4. Sol HighとGLM

目的はSol Highの品質判断を維持しながらSol High側のトークン消費を減らすこと。

- Sol Highは、要求・完了条件、重要なarchitecture/責務/API/data model/依存方向/互換性、原因不明バグ、重要テスト観点、GLM packet、高リスク変更、最終採否を判断する。
- Sol Highは原則、repository一次探索、grep/呼び出し元追跡、通常の実装・test・lint・build、GLM調査の再実行、途中経過取得、全diffの無条件精読、reviewer検証済みの低レベル再検査を行わない。
- repository固有の調査・設計案・実装・test・lint・build・自己reviewはGLMへ委譲し、新規task・decision・fix・accept・resumeは原則`glm-parent-action`を使う。同一taskのSol判断・修正・再開ではworker/reviewer sessionを継続し、新規taskだけ新sessionにする。過去のGLM文脈をSol Highが再説明しない。
- 通常workerはGLM-5.3/high。初回低リスクreviewはGLM-4.7/high、高リスク・Sol判断後・自動修正後・明示fix後のreviewはGLM-5.3/highを一方だけ使う。Sol判断後のworker継続と明示fixはGLM-5.3/max。
- `glm-worker --authority`はbootstrap専用のlocal readであり`glm-execution.md`を先読みしない。それ以外の`glm-worker`/`glm-parent-action`を実行・待機する前に`~/.codex/instructions/glm-execution.md`、packetまたはstderr error JSONを受け取ったら`~/.codex/instructions/glm-packets.md`を読む。

## 5. 品質ゲート

USER_REQUEST・`SPECIFICATION.md`・既存`AGENTS.md`・直前のSol判断で未確定の次はGLMだけで最終確定しない。

- architecture/責務/公開API・CLI/data model・永続化形式/依存方向・新規外部依存/後方互換性
- 原因不明バグの根本原因、security・data破損・不可逆操作
- 未検証の外部成立性を本番設計の前提へ進めるGo/No-Go・撤退判断
- 複数案の選択が将来構造へ意味のある差を生む場合

これらは実装前`NEEDS_SOL_DECISION`または最終`NEEDS_SOL_REVIEW`でSol Highを通す。承認済み構成内の型/package/interface追加、作業分割、命名、明白な仕様違反修正、test追加、互換性を狭めない強化だけではSolへ戻さない。永続fileも、永続状態の意味変更・migration・既存形式/ユーザー状態との互換・rollback/recovery・upgrade破壊可能性で意味判断が必要な場合だけSol確認する。
低リスク変更は独立reviewer PASS後、Sol Highは圧縮packetで採否を判断し全diff精読を省略してよい。親Codexのquality gate command実行は`~/.codex/instructions/quality-gate-capability.md`に従う。

## 6. Codex自身による編集

- Codex自身は原則ソース・test・設定・documentを直接編集せず、GLMの変更に問題があればGLMへ差し戻す。小規模・機械的でも直接編集へ切り替えない。
- ユーザーがCodex自身による直接編集・直接実行を明示した場合だけ例外とし、必要時に`~/.codex/instructions/worker/`の該当規則と`~/.codex/instructions/direct-edit.md`を読む。
- 直接実行の許可は明示された行為・成果物・変更理由だけに限定し、同一session/目的/releaseや運用・release・deploy・live確認の許可から新たな設計・実装変更へ拡張しない。

## 7. 必要時だけ読む規則

- Git履歴操作 → `git.md`、backup/大容量一時data → `backup.md`、AGENTS変更 → `agents-management.md`
- `glm-worker --authority`以外のGLM実行・待機 → `glm-execution.md`、task開始/再開のuser指示とrun-control → `task-request-boundary.md`、packet/WORKER_ERROR → `glm-packets.md`
- GLM rate limit再開 → `glm-auto-resume.md`、親Codex 5h Limit再開 → `codex-auto-resume.md`、停止/中断task/`--stop`/`--isolate` → `glm-stop-isolate.md`
- 外部成立性 → `feasibility-gate.md`、安全停止・子task終端/親USER_REQUEST完了 → `task-lifecycle.md`、原因不明runtime failure → `failure-evidence.md`、escaped bug/review原因層 → `escaped-cause-layer.md`
- Codex例外直接編集 → `worker/`該当file + `direct-edit.md`、quality gate → `quality-gate-capability.md`、repo-search → `glm-repo-search.md`

上記相対名は`~/.codex/instructions/`配下。
