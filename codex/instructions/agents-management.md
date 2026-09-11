# AGENTS系ファイル管理
- user-global `$CODEX_HOME/AGENTS.md`（default `~/.codex/AGENTS.md`）はユーザー所有とし、このrepositoryのinstall / upgrade / uninstallで置換・削除・追記しない。
- このrepositoryではroot `AGENTS.md`がCodexのsupported project-scoped bootstrapであり、parent/tool rule本文の唯一の正本は`codex/AGENTS.md`とする。rootには同じrule本文を複製せず参照だけを置く。
- `glm-codex-context`で他repositoryへglm-worker用contextを有効化する場合は、tool-owned project-scoped `.codex/config.toml`の`developer_instructions` bootstrapからinstalled namespaced instructionへ到達させる。既存repository `AGENTS.md`のsupported discoveryを置換せず、global AGENTSへruleを戻さない。
- 同じ意味のルールを複数箇所へ重複させない。
- 常時不要な詳細規則はglobal AGENTSへ戻さず必要時だけ読むinstructionsへ置く。
- GLMだけが必要な実装規則は`$CODEX_HOME/instructions/worker/`または`~/.glm-worker/`へ置く。
- 変更確定前に全文と参照先を確認する。
- 汎用ルールをユーザー承認なしに勝手に追記しない。
