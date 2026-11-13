# セットアップの補足

基本の導入・利用手順は[README](../README.md)を参照してください。

## 認証

インストールはClaude Codeの接続先をZ.aiへ設定します。APIキーは配置しないため、実行するシェルから`ANTHROPIC_AUTH_TOKEN`など、Claude Codeが使用する認証情報を渡してください。

対話的なbashでは、キーを表示せずに入力できます。設定後は同じシェルから作業を開始してください。

```bash
read -r -s -p 'Z.ai API key: ' ANTHROPIC_AUTH_TOKEN
printf '\n'
export ANTHROPIC_AUTH_TOKEN
```

接続先とモデルの管理設定は[`claude/settings-managed.json`](../claude/settings-managed.json)を正とします。

## 配置先の変更

`install.sh`と`uninstall.sh`は同じ環境変数で配置先を選びます。

| 対象 | 指定方法 |
| --- | --- |
| CLI | `GLM_WORKER_BIN_DIR` |
| Codex設定 | `CODEX_CONFIG_DIR`、未指定なら`CODEX_HOME` |
| Claude設定 | `CLAUDE_CONFIG_DIR`、設定ファイルを直接指定する場合は`CLAUDE_SETTINGS_FILE` |

例えばCLIだけを別ディレクトリへ配置する場合は、次のように指定します。そのディレクトリも`PATH`へ追加してください。

```sh
GLM_WORKER_BIN_DIR=/任意の配置先 ./install.sh
GLM_WORKER_BIN_DIR=/任意の配置先 ./uninstall.sh
```

## 撤去でエラーになった場合

管理ファイルがインストール後に変更されていると、撤去は上書きや削除を拒否します。エラーに示された対象を確認し、変更を保全したうえで管理内容を修復して再実行してください。別のinstall/uninstallと競合した場合は、その操作の終了後に再実行します。

撤去にはローカルのGo toolchainと既存のmodule cacheが必要です。通常のインストールやnetwork downloadは行いません。
