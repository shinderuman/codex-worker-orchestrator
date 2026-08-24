# GLM結果処理

`glm-worker`からpacket(stdoutのmachine JSON 1行)またはprocess失敗(stderrのerror JSON 1行とnon-zero exit)を受け取った場合に適用する。

## 共通

- 受理結果のmachine protocolはcompact 1行JSONで、keyは`status`・`risk`・status別契約field(`summary`・`decision`等のschemaと同じ語彙)・`targets`・`artifacts`・`sol_question`。空field・空配列のkeyは省かれる(`artifacts`key欠落=none、`targets`の`["none"]`=対象なしsentinel)。契約外のfieldは機械出力へ混入しない。
- `artifacts`のkeyがあるなら、要求・判断・報告に必要な成果物だけを記載パスから確認し、packetへ全内容を転載しない。
- 原因不明runtime failureの診断に必要なevidenceを求めた依頼では、`artifacts`参照先を`~/.codex/instructions/failure-evidence.md`の受理条件で必要範囲だけ確認する。

## `"status":"NEEDS_SOL_DECISION"`

- `decision`・`evidence`・`options`・`recommendation`・`test_obligations`を評価する。
- packetで足りるならリポジトリを再探索しない。判断不能な場合だけ`targets`に限定して現物を確認する。
- 判断後は元依頼を再記述せず、判断本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--decision-stdin <payload-bytes>`）で同じworker sessionへ継続する。

## `"status":"PASS"`

- 圧縮packetについて、要求との意味的一致・要求漏れ・矛盾・残余リスクを評価する。
- `"risk":"LOW"`かつ不整合・不確実性がなければ、GLMの調査をやり直さず全diffも読まない。
- PASSを機械的に信用せず、圧縮された意味情報への最終判断はSol Highが行う。

## `"status":"NEEDS_SOL_REVIEW"`

- `targets`と`sol_question`に限定して実コードまたはdiffを確認する。
- 修正が必要ならCodex自身で編集せず、修正方針本文を`~/.codex/instructions/glm-execution.md`のstdin mode（`--fix-stdin <payload-bytes>`）で同じworker sessionへ差し戻す。修正後は独立reviewerまで自動再実行される。

## `{"error":{"kind":"worker_error",...}}`

- stderrのerror JSON(`kind`・`message`・`detail{phase,exit_code,output_tail}`)とnon-zero exitで示される。エラー要約を確認し、無関係なリポジトリ調査をSol Highが代行しない。
- session破損が明示されている場合だけ`glm-worker --reset`後に再実行する。
