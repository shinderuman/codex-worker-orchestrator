# Task: インストール済み管理対象を安全に撤去するアンインストーラ

## Original instruction

2026-11-05 ユーザー指示（原文）:

````text
現行のインストールスクリプトのようにアンインストールスクリプトを作るというIMPLEMENTATION_PLANを作ってPRをSquash Mergeしてくれ
それもさっきのリストに登録してくれ
````

## Amendments

2026-11-05 ユーザー追加指示（原文）:

````text
AGENTS.mdみたいなものを復元しようとかしなくていい
````

## Resolved references

- 「現行のインストールスクリプト」はrepository rootの`install.sh`。現行install ownerは`glm-worker/internal/cliinstall`、`glm-worker/internal/codexinstall`、`glm-worker/internal/settingsmerge`、`scripts/manage-pull-hook.sh`。
- 「さっきのリスト」はWeb GPT実装候補の管理Issue #1510。実装要求の正本はこのTaskと現行Gitにあり、Issueの管理状態をCodex/GLMのTask authorityとして扱わない。
- `install-quality-tools.sh`は`install.sh`とは別の、明示実行する品質ツール導入コマンド。`install.sh`で配置した管理対象の撤去を本Taskの基本責務とし、独立導入された共用品質ツールを所有権の証拠なしに削除しない。

## Purpose

`./install.sh`で配備したCodex Worker Orchestratorの管理対象を、対応する`./uninstall.sh`で安全に撤去できるようにする。ユーザー所有の設定、他ツール、作業データを保持しながら、再インストール可能な状態へ戻す。

## Contract

- repository rootに`uninstall.sh`を追加し、`install.sh`と対応する実行入口・パス解決・環境変数を使用する。アンインストールのために通常インストール、全runtime build、network download、追加AI callを必要としない実装を優先する。
- `install.sh`の現行実際の配備対象を確認し、**所有権を確認できたものだけ**撤去する。対象は管理バイナリ（`GLM_WORKER_BIN_DIR`、既存`repo-cli-install -mode retire`）、Codexのmanaged instruction/rule/promptと`config.toml`の管理キー（`CODEX_CONFIG_DIR`/`CODEX_HOME`、Codex install-state）、Claudeの`settings.json`に統合した管理設定・env overrideとその管理state（`CLAUDE_CONFIG_DIR`/`CLAUDE_SETTINGS_FILE`等）、およびこのrepositoryへ配備したGit hooks（既存`manage-pull-hook.sh retire`）を含む。
- 各ownerの既存ownership state、hash、lock/transaction/recoveryを再利用する。未所有・不明・改変済みのfile/setting/実行体、他のGit hooksやユーザー設定を消去・上書きしない。インストーラーが配置したCodexのAGENTS.md相当の管理instruction/rule/promptは、所有権が確認できれば削除するだけでよい。アンインストール前のファイルを再構築・復元するためのbackupやsnapshot機構を追加しない。設定はインストーラーが所有する変更だけを取り除き、既存のユーザー設定は保持する。独立管理の`install-quality-tools.sh`成果物は根拠なく消さない。
- `~/.codex`、`~/.claude`、`~/.glm-worker`のdirectory丸ごと削除、session/telemetry/bundle/credential等のユーザーデータ削除を行わない。所有権を確認した管理記録と、空になった専用管理directoryだけを安全に片付ける。
- インストールなし・繰り返しアンインストール・部分インストール・中断からの再実行を定義し、意図した未変更と未撤去・失敗を区別して報告する。複数ownerにまたがる失敗時も検証済みの管理状態を失わず、後続再試行で復旧できるようにする。対抗するinstall/uninstallとsymlink/パス越境を安全に拒否する。
- 利用者向けREADMEに実行方法と撤去対象・保持対象を簡潔に記載し、既存の設置経路や通常installを壊さない。不要な別のinstaller framework、第二ownership DB、恒久互換shimを作らない。

## Must not

- ownership stateがないファイルを名前一致やパス一致だけで削除しない。管理fileが改変済みでも無断で強制削除しない。
- Codex/Claudeの設定全体、別のhook、他のCLI、独立導入の品質ツール、ユーザーのsession/credential/保存証拠を一括削除しない。AGENTS.md相当のファイルや従前の設定状態を復元するための新たな仕組みを作らない。
- アンインストールを正常と報告するために失敗や残存管理対象を黙殺しない。安全上の検証をforceや`rm -rf`で迂回しない。
- 現ACTIVEや別Taskの変更を混ぜず、既存のinstall/recovery contractを不必要に再設計しない。

## Acceptance criteria

1. `./install.sh`が現行で配置する全管理対象とownership stateの一覧が実コードと対応し、`./uninstall.sh`でインストーラー所有物だけを撤去できる。既存のユーザー設定は維持し、AGENTS.md相当のファイルや過去の設定状態の復元を成功条件にしない。
2. temp HOME/環境変数で、fresh install→uninstall、uninstallの再実行、uninstall→reinstall、各管理ownerの部分欠損・中断・再試行を検証する。
3. ユーザー変更済みCodex/Claude設定、同名の非所有バイナリ、既存の独立hooks、外部変更した`core.hooksPath`を保持する。symlink、state改ざん、path越境、並行操作では破壊的削除・虚偽の成功が起きない。
4. Codex/Claudeの管理キーや管理ファイル以外の値、品質ツール、runtime dataは維持される。削除した管理対象について対応するownership記録が矛盾なく処理される。
5. shellcheck/shfmt、関連Go test、既存install/hook/settings smoke、アンインストールの正負シナリオが成立し、変更後の再installが通常どおり機能する。未実行の検証は明示する。
6. READMEと実行入口・差分が一致し、CodexがDraft PRで最終レビュー・採否判断できる。対象Taskの終了Metadata同期とPublicationはCodexに引き継ぐ。

## Historical invariants

- インストール済み管理対象のownerとsource of truthは現行のinstaller/ownership state。元のrepositoryとGit履歴、通常Taskの管理Metadataは変更しない。
- ファイルの意味契約、既存ユーザー設定、Git authority、source-bound evidence、CLI installとhookの現行保護を保つ。

## Dependencies

none
