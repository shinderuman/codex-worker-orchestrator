# Task: provider復旧probeの停止境界

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
ClaudeRunner.Probeは通常Runの停止確認・process group終了経路を使わずexec.Command(...).Run()で同期実行する。AttachStopController済みでRequest()した後でも子processを起動し、成功を返すことをfake CLIで再現した。復旧probeにも停止要求を適用し、provider無応答時に正規停止を妨げないようにする。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/runner/probe.go`の`Probe`、`runner.go`の`Run`/`runCommand`、`process_group_unix.go`、`workflow/model_call.go`の`recoveryLoop`/`runRecoveryProbe`が対象。
- recoveryLoopのdeadlineはprobe呼出前後の判断であり、同期実行中のProbeを打ち切らない。実provider障害の発生頻度や無応答時間は未測定。
- 停止済みcontrollerを付けてProbeを呼ぶ一時testは、`error=nil, child_started=true`で失敗した。実model呼出しはしていない。
- 今回はレビューと計画化まで。実装開始を意味しない。

## Purpose

復旧probe中もユーザー停止を尊重し、停止不能による手動process修復・親Codex介入を防ぐ。

## Contract

- 停止要求済みならprobeを起動せず、実行中に停止を受けた場合は子processとその子孫を既存の停止責務で終了させる。
- 中断を成功・通常provider error・新たな復旧retryへ読み替えない。task identityとresume可能なcheckpointを維持する。
- recovery deadlineとprobe実行時間の関係を明確にし、deadline到達後に同期呼出しが無制限に残る経路を解消する。新たな時間閾値が必要なら実装前に根拠と意味を確定する。

## Must not

- 実provider/model callを回帰testの前提にしない。
- 子processだけ終了して子孫を残したり、中断後に元のtaskを勝手に再実行しない。
- old protocol/schemaの互換経路、別daemon、別の停止状態正本を追加しない。

## Acceptance criteria

- 停止要求済みのProbeが子processを起動しないことをfake executableで確認する。
- 実行中停止と無応答probeでprocess終了・中断結果・checkpointが整合する。
- 正常probe、transient failure、provider terminal failureの既存分類とsession非保存を維持する。
- workflowの復旧loopとrunnerの停止処理を合成して検証し、各々のmock成功だけを根拠にしない。

## Historical invariants

- 通常callと復旧probeで停止authorityを分裂させない。
- current schemaのみを正規入力とし、Codex Reductionのために停止・復旧保護を削らない。

## Dependencies

none
