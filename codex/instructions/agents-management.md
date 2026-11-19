# AGENTS系ファイル管理
- user-global `$CODEX_HOME/AGENTS.md`（default `~/.codex/AGENTS.md`）はユーザー所有とし、このrepositoryのinstall / upgrade / uninstallで置換・削除・追記しない。
- このrepositoryではroot `AGENTS.md`がCodexのsupported project-scoped bootstrapであり、parent/tool rule本文の唯一の正本は`codex/AGENTS.md`とする。rootには同じrule本文を複製せず参照だけを置く。
- `glm-codex-context`で他repositoryへglm-workerを有効化する場合、当該repository固有の`AGENTS.md`を尊重し、このrepositoryの親Codex規則をユーザーレベル経由で注入しない。
- 同じ意味のルールを複数箇所へ重複させない。
- repository固有の詳細規則は本repositoryの`codex/instructions/`へ保持し、ユーザー設定へ複製しない。
- 汎用のGLM worker規則だけはツール資産として`$CODEX_HOME/instructions/worker/`へinstallし、repository固有の親ルールとは区別する。
- 変更確定前に全文と参照先を確認する。
- 汎用ルールをユーザー承認なしに勝手に追記しない。
