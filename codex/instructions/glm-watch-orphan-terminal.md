# GLM watchのorphan terminal recovery境界

`glm-worker --watch`が対象taskをmachine-owned orphan terminalとして終端した場合だけ適用する。通常のwatch待機・完了待機は`glm-execution.md`を正とする。

## machine boundary

- orphan判定、error material選択、repository lock probe / lease / lease内再検証、terminal eventのstatus・exit semantics・recovery fieldsは`control:orphan-watch-terminalization`とcurrent `watchOrphanTerminal` / `buildWatchOrphanExitEvent`を正とする。親はexact判定順・field schema・retry procedureを自由言語から再構築しない。
- ownership/stateをmachineが確定できない場合はorphanとみなさない。PID、lock file本文、mtime、process名、経過時間等から親がorphanを推測してfail-closed境界を迂回しない。
- `orphan-terminal`は正常task完了ではなく、実行ownerを失ったstateを親へ返すrecovery/reporting control outcomeである。task state・worker/reviewer session・resume checkpointをreset / deleteせず、既存stateを保持する。

## 親のrecovery判断

- `watch_exit`が投影する`consistent` / `inconsistency` / `required_action` / `allowed_actions` / `resume_kind` / `last_material` / `artifact_dir`をmachine evidenceとして読む。同じstateのhandoff recoveryと語彙・意味を合わせることはproduction owner/testを正とし、親側で別schemaへ読み替えない。
- `consistent: false`ではnext actionを推測・実行しない。machineがactionを示さない場合も、同じstateへwatchを再attachして進捗を捏造せず、Sol/userへの報告またはcanonical recovery pathの意味判断に留める。
- 同じstateの再開・修正が必要な場合は`glm-execution.md`・`glm-stop-isolate.md`等の既存正規経路を使う。orphan terminalization自体をtask completion、session ownership、detached parent-wait ownershipの証明へ拡張しない。

## residual boundary

- machine controlはorphan terminalizationの成立条件とbounded recovery projectionを強制するが、得られたevidenceをどう解釈し、修復・再開・中止・user報告のどれを選ぶかは親が判断する。
