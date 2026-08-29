# GLM実行中の待機

`glm-worker`の通常task・resume・review/fix/decisionを起動した後、processの完了を待つ間だけ適用する。

- `~/.codex/config.toml`の`background_terminal_max_timeout=21600000`msを前提とし、起動済みprocessは利用可能な最大待機時間のblocking waitで待つ。
- runningであることを確認するだけの定期status/process pollを行わない。状態が変わっていないことだけを伝えるuser-visible進捗報告も行わない。
- 完了、Sol判断/review、guard stop、rate/provider stop、user割り込みなど意味のある状態変化で制御が戻った時点から次の処理へ進む。
- tool sessionが中断した後の復旧、failure診断、制御復帰時の状態が曖昧な場合は、必要な`glm-worker --status`等を実行してよい。定期liveness確認の代用にはしない。
- 待機のためのtimer、background watcher、追加model call、runtime telemetryは追加しない。
