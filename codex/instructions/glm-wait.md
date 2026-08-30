# GLM実行中の待機

`glm-worker`の通常task・resume・review/fix/decisionを起動した後、processの完了を待つ間だけ適用する。

- `~/.codex/config.toml`の`background_terminal_max_timeout=21600000`msを前提とする。code-modeで長時間の`glm-worker`を起動・待機するcellは外側cell先頭へ`// @exec: {"yield_time_ms":21600000,"max_output_tokens":1000}`を指定し、cell自体が短いyieldで`functions.wait`へ返ることを避ける。
- 内側の初回`tools.exec_command`はhost側で短くyieldしてsession IDを返すことがある。processがrunningなら同じcode-mode cellを終了せず、そのsession IDへ空の`tools.write_stdin`を`yield_time_ms=21600000`で呼び、`background_terminal_max_timeout`の待機境界を使う。空の`write_stdin`がhost制約でrunningのまま返った場合も、同じcell内で同じsessionを継続して待ち、Solへunchanged runningだけを返さない。
- 外側code-mode cell自体が指定値より短くclampされて`functions.wait`へ制御を返した場合だけ、利用可能な最大`functions.wait`で同じcellを継続し、そのruntime制約を事実として扱う。
- runningであることを確認するだけの定期status/process pollを行わない。状態が変わっていないことだけを伝えるuser-visible進捗報告も行わない。
- 完了、Sol判断/review、guard stop、rate/provider stop、user割り込みなど意味のある状態変化で制御が戻った時点から次の処理へ進む。
- tool session中断後の復旧、failure後の制御復帰、現在の次操作が曖昧な場合は、まず`glm-worker --handoff`を実行し、`consistent`・`required_action`・`allowed_actions`を次のlifecycle操作の正規入口とする。`consistent:false`では操作を推測せず停止する。`repository_lock`・`task_liveness`等の詳細診断が必要な場合だけ追加で`glm-worker --status`を使い、statusやpacket proseから合法な次操作を再構成しない。
- 待機のためのtimer、background watcher、追加model call、runtime telemetryは追加しない。
