# Task: 完了判定を2度誤った検知失敗の重大インシデントを分析・是正する

## Original instruction

````text
俺には2重にJSONが返っているように見えるんだがこれは表示だけの話なのかそれとも本当に2重に返っているのか
もし2重に返っているのならそのことよりもこれは過去2回完了報告を聞いているのになぜ直ってないのかそこが問題だ
いまはGLMが使えないと思うので一次回答をお前がしろ

これは直す必要があるのか
であればタスクとして積め
そしてそれ以外に2回修正を検知できなかったことを重大インシデントとして対応しろ
````

## Amendments

### 2026-08-24 user correction

````text
「2回返すこと」は重大インシデントではない
「過去2度検知できなかった」のが重大インシデントだ
そこを履き違えるな

これが表示上の問題で例えばCodex Desktopのバグだったりするのならば対応する必要はない

全てにおいて最重要なのは最上位目的の `Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。` である

そのためであればこのインシデントも些細なことだ
二重にJSONを返してそれを受領していることは無駄なトークンの元だと思っているがそうじゃないなら優先度はお前が判断しろ
````

### 2026-08-24 retention requirement

````text
もし実害がない場合はこの現象、状況を忘れないようにBLOCKEDとかあるいは新しい境界のタスクとして残せ
````

## Resolved references

- 「2重にJSON」はTask 007 task ID `173436e3-633c-493d-a6c7-9816704f0888`の初回`NEEDS_SOL_REVIEW` machine JSONが、同じcommand cardに同一内容で2回ユーザー可視表示された事象を指す。
- 該当実行のFunctions store `glm_terminal_task007_resumed`は2,714 bytes・2 physical linesで、summary substring一致は1件だけ。保存raw stdout上のaccepted terminal resultは1件である。
- telemetryでは同じresult本文はreviewer call ID `09181ac1-6c12-4219-a0f5-850734d6d461`・response SHA-256 `0dcecf064882084582fd3d654913caab2e4c775e9a2496a9017a53213652ac0b`の1件だけ。model/reviewer二重実行ではない。
- 「過去2回完了報告」は、(1) repo内emit 1件とcaller境界特定を解消と誤認した完了報告、(2) store→captured marker→sync load contract・模擬fixed Eval・一度のlive positiveを完了証拠とした報告を指す。
- 今回の再現でも規定のbackground exec→internal store→captured marker→sync loadを実施したが、Codex Desktop/tool表示面と同期load表示面で同じraw payloadが二面描画された。exact renderer surfaceはCodex app terminal session非接続のため現時点で内部logから一意に特定できないが、producer rawとtelemetryが各1件なので重複は親tool/UI境界にある。
- 親Codexが受け取ったtool outputは同期loadの1件だけで、nested execはcaptured markerだけをmodel側へ返した。現時点では同一JSONがCodex model contextへ2回流入した証拠はなく、確認できているのはDesktop上の二重描画だけである。

## Purpose

同じ不具合を2度「解消済み」と判定したcompletion detectionのfalse negativeを重大インシデントとして分析し、最上位目的に影響する品質劣化を見逃さない完了判定へ是正する。二重描画自体は、Codex実消費またはQuality Deltaへ影響する証拠がある場合だけ修正対象にする。

## Contract

### Incident classification

- major incidentの対象は二重描画ではなく、明示acceptance違反を2度検知できず「解消済み」と報告したcompletion detection / final acceptanceの反復失敗である
- 二重描画は現時点でDesktop表示上の低影響defect候補とし、重大度をincident本体へ転嫁しない
- 判断基準は最上位目的のCodex ReductionとQuality Delta。表示美観や文書整合だけを優先しない

### Token / quality impact gate

- 同一terminal payloadがCodex model context・永続conversation context・parent packet処理へ実際に2回流入したかを、tool call/output record・actual usageが取得可能ならそれで判定する
- Desktopに2回描画されてもmodel contextへのtool outputが1件なら、二重描画修正は原則行わずCodex Desktop側の外部表示問題として記録だけにする
- model contextへ2回流入している場合だけ、追加input token・compaction pressure・parent processingへの影響を測り、Codex Reductionへ有意なら具体修正を実施する
- actual usageを観測できない場合は二重token消費を推測せずunknownとする
- 二重流入なし・実害なしと判定した場合も現象を完了taskと一緒に消去せず、`IMPLEMENTATION_TASKS/desktop-terminal-payload-double-render-boundary.md`をBLOCKEDの既知外部境界として維持する

### Root cause and escaped-detection audit

- 第1 false-complete: 完了条件を「ユーザー可視1回」から「repo内emit 1回・原因境界特定」へ狭めたauthority errorを一次証拠で確定する
- 第2 false-complete: 模擬testと単発live positiveが実Desktop rendererの継続的production enforcementではないのに、instruction contractを強制済みpostconditionとして採用した原因を確定する
- worker/reviewer/Sol/parent final gateのどこで`contract/prompt ≠ production enforcement`を見落としたかを分離する
- `IMPLEMENTATION_HISTORY.md`・`EVAL.md`・`glm-execution.md`の「解消済み」「live positive」「残余risk」記述を現実装・今回の再現へ照合し、虚偽またはobsoleteな完了証拠を撤回する
- 完了済み項目のうち、親instructionまたは単発live観測だけをproduction enforcementの根拠にした類似項目をboundedに監査し、具体的な同型riskが一次証拠で見つかった項目だけ再openする。一般的な全履歴再レビューへ拡張しない

### Detection remediation

- 完了条件、観測可能なproduction postcondition、検証証拠のauthorityを分離し、instruction文面・模擬test・単発live positiveを継続的production enforcementと同一視しない
- 対象がrepository外のDesktop表示だけなら、repo内で強制不能なacceptanceを解消済みと報告せず、最上位目的への影響なしを確認して対応不要へ分類する
- parent final gateが「要求違反は残るが上位目的へ影響しないため非対応」と「要求を満たした」を区別して記録できる最小contractへ収束する
- 既存の一般rule/promptへ広範なchecklistを追加せず、今回の2 false negativeを生んだauthority/evidence gapへだけ対処する

## Must not

- repo内emit 1件、telemetry 1件、原因境界特定だけで完了扱いしない
- 模擬Go testやinstruction文面だけを実Desktop表示の代替証拠にしない
- 一度だけ成功したlive runを継続的保証と同一視しない
- structured JSON化、payload hash、同文比較だけで二重描画解消とみなさない
- `glm-worker`へblind dedupeを追加して正当な別resultを隠さない
- incidentを二重描画修正だけ、またはworker/reviewer checklist追加だけで閉じない
- Desktop表示だけでCodex model contextへ二重流入しないと判明した場合、表示修正を目的化しない
- 最上位目的への影響が観測できないのに新しいcapture framework・daemon・dedupe層を追加しない
- Task 007の未完了diffを同じcommitへ混ぜない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- 今回再現についてproducer stdout 1件・telemetry result 1件・親Codex tool output 1件・ユーザー可視2件の層別evidenceを保存
- Codex model contextへの二重流入有無とCodex actual token impactを判定し、未観測ならunknownを維持
- 過去2回の完了判定それぞれについて、誤った完了証拠・見逃し層・なぜ既存review/Sol gateが拒否しなかったかを記録
- `IMPLEMENTATION_HISTORY.md`の解消済み項目を再openし、`EVAL.md`のobsoleteなlive positive完了証拠を修正
- 二重流入ありなら、修正後にCodexが同じmachine resultを1回だけ受け取り、Codex Reductionへの改善を測定
- 二重流入なし・Desktop表示だけなら、repo/Codex orchestration修正を行わない判断と根拠を記録
- 二重流入なし・実害なしの場合も、既知現象・層別evidence・再調査activation条件がBLOCKED taskに残っていることを確認
- completion detectionの是正をproduction evidenceで検証し、模擬testだけに依存しない
- 類似false-complete監査はbounded evidence付きで完了し、該当なしならその結果を記録
- containment、rollback、Desktop変更時の再検知条件を明示
- 必要なtest/review/Sol gate後、親CodexがTask 007と独立commit

## Historical invariants

- 最上位目的はSol High相当のQuality Deltaを維持しながらCodex / Sol実消費量を大幅に削減すること
- 1 accepted terminal resultにつき、親Codexのmachine result受領・packet処理は1回。ユーザー可視回数はtoken/quality影響と分離して扱う
- `glm-worker`のaccepted result stdout・保存raw・telemetryは今回各1件で、二重model call/二重emitを示さない
- EVALのcaller側除外撤回は維持するが、既存fixed Evalと単発live positiveは今回再現を防げなかったため完了証拠として不十分

## Dependencies

none

## Review findings

- 過去2回のfalse-completeを検知するproduction postconditionと証拠authorityの区別が存在しない
- 二重描画がCodex model contextへ二重流入した証拠は現時点でなく、表示修正の要否はimpact gate未判定

## Current boundary

completion detectionの反復false negativeを重大インシデントとしてopen。Task 007のrate-limit resumeを壊さないため最優先NEXTへ積み、Task 007局所終端後にACTIVE化する。二重描画修正はCodex actual token / Quality Deltaへの影響を確認してから判断し、実害なしでも現象自体は独立BLOCKED taskへ保持する。
