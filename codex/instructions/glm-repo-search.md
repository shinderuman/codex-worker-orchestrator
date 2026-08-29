# repo-search featureの制御

`glm-worker`のrepo-search feature flagとread-only CLIの運用境界だけを保持する。BM25 core・routing・telemetryの実装契約はproduction codeと対応するtestが正である。

## feature flag

- `GLM_WORKER_REPO_SEARCH`環境変数の真偽値で制御し、既定はenabled。不正な値は設定読込時にfail closedする。
- 切替対象はworker promptへのBM25 navigation候補注入とreviewerの独立BM25 search。disabled時は両者を実行せず、repo-search導入前の通常repo inspectionへ戻る。
- reviewerのdiff changed-path navigationとexhaustive search proofは品質境界として常時維持され、このflagでは無効化しない。
- exhaustive proofを実際に要求するPlan taskでは、Original instruction / Amendments / Contract / Acceptance criteriaのいずれかへ `EXHAUSTIVE_SEARCH_REQUIRED: true` を独立した行として明示する。`exhaustive`等の自然言語や「proofを無効化しない」という保存要求だけではactivationしない。Planのないrepoでは同じmarkerを依頼本文の独立行として使う。

## CLI

- `glm-worker --repo-search <query>`はcurrent repositoryを既存BM25 coreだけでread-only検索し、機械可読JSONを返す。model呼出・state変更・repository lock取得を行わない。
- flag disabled時は検索を実行せず、disabledを明示するJSONを返す。
- 出力の`results`はBM25上位候補(path・line・score)でありnavigation-onlyである。コード確認の代替にせず、対象の現物を確認してから扱う。
