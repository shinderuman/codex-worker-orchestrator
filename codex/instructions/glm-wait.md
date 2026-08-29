# GLM実行中の待機

`glm-worker`の通常task・resume・review/fix/decisionを起動した後、processの完了を待つ間だけ適用する。

- `~/.codex/config.toml`の`background_terminal_max_timeout=21600000`msを前提とし、起動済みprocessは利用可能な最大待機時間のblocking waitで待つ。
- runningであることを確認するだけの定期status/process pollを行わない。状態が変わっていないことだけを伝えるuser-visible進捗報告も行わない。
- 完了、Sol判断/review、guard stop、rate/provider stop、user割り込みなど意味のある状態変化で制御が戻った時点から次の処理へ進む。
- tool session中断後の復旧、failure後の制御復帰、現在の次操作が曖昧な場合は、まず`glm-worker --handoff`を実行し、`consistent`・`required_action`・`allowed_actions`を次のlifecycle操作の正規入口とする。`consistent:false`では操作を推測せず停止する。`repository_lock`・`task_liveness`等の詳細診断が必要な場合だけ追加で`glm-worker --status`を使い、statusやpacket proseから合法な次操作を再構成しない。
- 待機のためのtimer、background watcher、追加model call、runtime telemetryは追加しない。
