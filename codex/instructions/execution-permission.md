# 親実行permissionの収束とexecution denialの扱い

親Codexが自身のcommand実行でCodex実行基盤のapproval reviewer・sandbox・外部安全審査の判定を受けた場合だけ適用する。GLM worker/reviewer session内部の権限と品質gate契約は対象外である。

## state分離

- task authority(userがACTIVE taskへ与えた継続authority)、execution permission(execpolicy ruleと審査layerのapproval状態)、execution result(個別commandの成功失敗)を別stateとして扱い、暗黙変換しない。
- 外部実行層のdenialをuser denial・user authority欠如へ変換しない。command-failed・execution-denied・permission-required・user-denied・user-input-required・task-blocked・task-terminalを同一視せず、execution deniedだけでawait-user・親USER_REQUEST完了・task terminalへ自動遷移しない。
- ACTIVE taskを継続するuserのrun-control指示は`~/.codex/instructions/task-request-boundary.md`の境界に従い、permission grantにもpermission denialにも解釈しない。

## canonical permission state

- 恒久実行permissionの正はinstall-managed execpolicy rule(`~/.codex/rules/`)へ収束させた決定論的allowだけである。審査layerのsession localな承認記憶や過去の対話上の許可をpermission stateとして再利用しない。
- canonical completion operationのinstall本配置は、managed ruleでallow済みの`glm-parent-action install`として実行する。このactionはtask admission(awaiting-parent-completion中のみ)とtrackedなrepository harness opt-in marker(`.glm-worker-repository-harness`)がactiveなrepository限定で、repository root直下のGit tracked・regular・non-symlinkな`install.sh`だけをcwd=repository rootで受理する。追加引数・任意path・shell form・同一lifecycle stateを持つ他repositoryのscriptは受理しない。直接の`./install.sh`実行は従来どおり正規approval対象であり、permission収束の対象にしない。
- `glm-parent-action install`はguard拒否を`install_guard_rejected`、install childのnon-zero・起動不能を`install_failed`としてtask lifecycle statusと機械的に区別されるstructured execution resultで返す。`install_failed`はexecution resultであり、user denial・permission不足・await-user・task terminalへ変換せず、install原因を解決して同一taskを継続する。
- 同一ACTIVE task内の同一operation class・同一authority scope・同一execution boundaryで一度成立したpermissionは、permission-relevant state changeなしに再交渉しない。rule fileはhost-localでありsession rotationで失われない。
- execpolicyでallow済みのcanonical completion operation(install本配置・offline smoke等)を、毎回permission negotiationの対象として再判断しない。

## denial時の判定

- 従来成功していたoperationが突然denialされた場合、すぐにuserへ承認を求めない。前回成功時とのpermission-relevant state差分(task authority・session identity / rotation・approval state・sandbox profile・execution policy・permission profile・runtime実行境界)を先に確認する。
- 差分を確認できない場合は「permission不足」と推測して再承認を求めない。外部審査layerの非観測状態をtask state・session・telemetryへ複製せず、denialはそのcommandのexecution resultとして扱い、同一scopeの正当操作または同じcommandの再実行へ継続する。
- 差分が実在する場合だけ、何が以前と違うか・なぜ既存permissionでは不足するか・どのscopeへのpermissionかを示して正規approval手続きを取ってよい。成立したpermissionは同一scopeで保持し、再要求には前回成立後のpermission-relevant state change証拠を要する。
- 対応の結果、新たな権限・Codexの外で変わる外部状態・意味のあるuser判断が本当に必要な場合だけ停止する。停止条件は`~/.codex/instructions/task-lifecycle.md`と同じ境界である。

## user messageの解釈

- 「今までできていた作業をそのまま続けろ」は既存task authorityを維持するrun-control指示であり、新permission grant・permission denial・task変更のいずれでもない。
- 「なぜまた許可を求めて作業を止めている」等の批判・指摘は、permission grant・permission denial・新task指示・停止指示へ変換せず、ACTIVE taskを継続する。
