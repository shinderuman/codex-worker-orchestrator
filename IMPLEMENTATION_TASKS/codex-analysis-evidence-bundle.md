# Task: glm-worker bundleへCodex親実行証拠を統合する

## Original instruction

````text
今後のtask監査でglm-worker/Claude側だけでなく、親Codex・Guardian・Codex host/runtimeの一次証拠も1つのanalysis bundleから取得できるようにする。

このtaskは実装前に、実行中のMac上の`$CODEX_HOME`を親Codex自身がread-onlyで調査し、このtaskに列挙されたCodex-local evidence sourceの実在・配置・schemaを確認する。task記述上の想定だけからcollectorを実装しない。

External feasibilityが`status: observation`の間はglm-workerへ実装dispatchしない。親Codexの実機調査で成立性を確認した後、producer evidenceと親Go判断を記録して`status: implementation`へ更新してから通常Codex + GLM workflowを開始する。
````

## Amendments

none

## Resolved references

- 既存`glm-worker bundle [task-id]`をcanonical analysis artifactとする境界を維持する。
- retained production evidenceで確認されたparent Codexのwait/re-entry、Guardian escalation、sandbox/tool retryの監査必要性を、このtaskの収集対象が必要になった実運用証拠として扱う。

## Purpose

`glm-worker bundle`だけで、logical taskのglm-worker/Claude証拠に加え、そのtaskを管理した親Codex sessionと必要なCodex host/runtime証拠まで後から監査できるようにする。temporary `glm-watcher bundle`や手作業のCodex session探索へassociation責務を残さない。

## External feasibility

status: observation
assumption: このtaskに列挙されたCodex-local evidence sourceの実在・配置・schemaは現在のMac上のCodex実体で未確認であり、実装前に親Codexが実producerをread-only観測して確定する必要がある

## Feasibility procedure

親Codex自身が、現在実行しているMac上で以下をread-onlyに確認する。

- `$CODEX_HOME`の実値
- `sessions` / `archived_sessions` の実在、rollout `session_meta` のparent/Guardian識別field
- `logs_2.sqlite`の実在と、対象thread/time rangeを安全に抽出するために必要な実schema
- rolloutからlocal attachmentへ辿るstructured referenceと、対応するattachment storageの実在/配置
- `process_manager/chat_processes.json`または現行同等surfaceの実在/schema
- `background_terminal_max_timeout`の現在の保存場所・値・安全な限定取得方法

確認結果はtask artifactまたはHistoryに、少なくとも「observed / absent / schema-different / unsupported」をsourceごとに残す。

このtaskに書かれたpath/schemaは実装前のcandidateであり、実機観測と違う場合は実機を正とする。存在しないsourceのために推測parserや互換layerを作らない。

実装へ進める場合、親Codexが`## External feasibility`を次の契約へ更新する。

- `status: implementation`
- `assumption:` 実装が依存する確認済み外部前提
- `evidence-source: producer`
- `evidence:` 実機で確認したpath/schema/association surfaceの要約
- `go:` 親CodexのGo判断

その更新前にglm-workerへimplementationをdispatchしない。

## Contract

- 既存`glm-worker bundle [task-id]`を拡張し、別の第二bundle commandを作らない
- task-scoped glm-worker/Claude evidenceと、複数taskを跨ぎ得るparent Codex session evidenceをmanifest上で明確に分離する
- parent rolloutは`session_meta`等のdeterministic metadataで選び、mtimeだけで選ばない
- Guardianはexplicit parent/thread metadataで関連付け、task監査に無関係なchildを無制限に収集しない
- historical taskでparent associationを確定できない場合は推測せずunknown/incompleteにする
- 親rolloutが参照するCodex-local attachmentが実機で確認できた場合、structured referenceから辿れる対象だけを含める
- app-server diagnostic log sourceが実機で確認できた場合、global DBを丸ごとコピーせず、対象threadとtask時間範囲へbounded extractionする
- current/in-flight taskのprocess-manager evidenceが実機で確認できた場合、matching parent/Guardian processだけをvolatile bundle-time evidenceとして含める
- `background_terminal_max_timeout`のような必要な非secret scalarはallowlist projectionだけを保存し、full configを含めない
- bundle manifestへevidence class、source/provenance、included/missing/unavailable/truncatedの状態を残す
- archive作成以外はread-onlyとし、model call、network transfer、task lifecycle mutation、repository mutationを追加しない
- stdoutは既存のmachine-readable JSON contractを維持する

## Must not

- `$CODEX_HOME`全体をarchiveへコピーしない
- `state_5.sqlite`、`logs_2.sqlite`、WAL、`config.toml`、`history.jsonl`、`session_index.jsonl`、`shell_snapshots`を理由なく丸ごとbundleしない
- `auth.json`、token、credential、secret-bearing configを含めない
- filesystem pathらしい文字列をrollout proseからgrepして任意fileをコピーしない
- repository cwd一致やtimestamp proximityだけでparent/task associationを確定しない
- parent thread全体をtask-scoped evidenceと偽らない
- absent/unknownなCodex内部schemaを想像してfuture compatibility frameworkを作らない
- `glm-watcher`へsession association/archive schemaを残したまま二重実装しない

## Acceptance criteria

- 実機Codex home調査結果がsourceごとに記録され、その観測に基づく実装になっている
- representative current/latest task bundleに、既存task/Claude evidenceとdeterministically associated parent Codex rolloutが含まれる
- relevant Guardian childがparentと区別されて含まれ、parentに属するだけの過去Guardianを無制限に収集しない
- parent/Guardian evidenceはtask-scoped evidenceとmanifest上で別分類される
- ambiguous parent candidateはsilent selectionせずunknown/incompleteになる
- 実機で確認できたapp-server log / attachment / process-manager / safe runtime-setting sourceはboundedかつprovenance付きで収集される
- 実機で存在しないsourceは推測実装せず、その状態がtruthfulに表現される
- secret/global Codex stateをarchiveへ混入させない
- `glm-watcher`等のcallerは最終的にremote `glm-worker bundle`を1回実行し、その1 archiveだけをtransferすればよい
- 既存task/Claude bundle association・atomic archive・read-only semanticsを壊さない
- repositoryが要求するlint/test/validationを完了し、独立reviewと必要なSol品質gateを通す

## Historical invariants

- Codex Reductionの監査対象はGLM callだけではなく、親Codexのwait/re-entry、Guardian escalation、tool retryも含む。
- raw diagnostic evidenceは有用だが、task provenanceとprivacy scopeを偽らない。
- evidence collection自体のためにmodel callを増やさない。
- 1 behavior / association contractにつきownerを1箇所に置き、watcherとglm-workerへ同じsession discovery logicを複製しない。

## Dependencies

- 既存`glm-worker bundle` implementation

## Review findings

none

## Current boundary

未着手。commentlint sandbox taskより先に、このtaskだけを通常Codex + GLM workflowで完了する。Task 021は同じrunで開始しない。
