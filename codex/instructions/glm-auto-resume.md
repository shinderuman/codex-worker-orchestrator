# GLM rate limit自動再開

`glm-worker`がstderr error JSON(`{"error":{"kind":"rate_limited",...}}`、exit 1)の`detail.limit: ZAI_GLM_CODING_PLAN_5H`で停止した場合だけ適用する。

## Codex wakeとの重複排除(coalesce判定)

- `detail.auto_resume_available: true`を確認し、同じtaskをreset後も継続するというsemantic intentが確定したら、以降のcoalesce判定から予約後verifyまでを1回のcode-mode/tool orchestrationで実行する。最初のnested tool callとして`REPO_ROOT`で`glm-worker --check-wake-coalesce <detail.auto_resume_at_rfc3339>`を1回だけ実行し、machine JSON 1行をそのorchestration内でparseする。このcommand自身が現在processの`CODEX_THREAD_ID`をcanonicalな親thread identityとして読み、`~/.codex/automations/codex-5h-wake-*/automation.toml`のid・name・status・target_thread_id・prompt内の親thread ID・one-shot rruleと`~/.codex/sqlite/codex-dev.db`の`automations.next_run_at`を読み取り専用で照合する。親Codexはthread IDを探索・copy・argvへ渡さず、coalesce結果を単独のSol turnへ返してから予約を再開しない。
- `decision: "coalesce"`は、現在の親実装taskへ「作業を続けろ」を送るACTIVEなCodex 5h wakeを機械確認でき、その次回発火時刻がGLM resume時刻以降10分以内であることを意味する。この場合は同じorchestration内でGLM resume automationの作成・更新・削除を一切行わず、coalesceしたwake automation ID・次回発火時刻・追加待ち時間だけをbounded resultとして返してrate limit停止の報告へ進む。次の「予約」へ進まない。
- `decision: "create_glm_wake"`(wake不存在・対象thread不一致・PAUSED・entity不正・時刻が早すぎる・遅すぎる・検証不能・複数一致)は、Solへ戻らず同じorchestration内で次の「予約」へそのまま進む。commandがusage errorで失敗した場合は引数を1回だけ修正して再実行し、それでも読めなければ予約へ進む。`CODEX_THREAD_ID`欠落・空・invalidによる`not_found`はidentityを推測せずfail closedし、予約へ進まない。coalesce判定の結果だけで親判断や会話memoryによって予約を省略・復活させない。
- 許容時間境界は固定値で記録する。coalesce条件は`GLM resume時刻 <= wake次回発火時刻 <= GLM resume時刻 + 10分`であり、下限はwakeがGLM reset前に発火してresumeを再びrate limit停止へ当てるのを防ぐ。上限10分は300分windowの3.3%・2026-08-26実測3分差の約3倍で、1 eventあたり親Codex turn 1回分の節約に対して許す追加待ち時間の上限である。この境界を自然言語の「近い」判断へ置き換えない。

## 予約

- 本節はcoalesce判定が`decision: "create_glm_wake"`を返した場合だけ適用する。`decision: "coalesce"`の場合はGLM resume automationを作らない。
- error JSONの`detail`に`auto_resume_available: true`・`auto_resume_at_rfc3339`・`auto_resume_key`・`task_id`・`repo_root`が揃っていることを確認する。
- coalesce後の既存automation lookup、`automation_update`のcreate/update、返り値検査、`--verify-auto-resume`、失敗時cleanupは同じcode-mode/tool orchestration内で順番にawaitし、各stageの結果を次stageへ機械的に渡す。中間結果を`text()`等でSol-visible outputへ流して親modelへ戻らず、正常時は最終verifyまたはcoalesceのbounded postconditionだけを返す。lookup/create/update/verifyの間へcommentary・reasoning・通常assistant turnを挟まない。
- 既存automation lookupも同じorchestration内だけで行い、同名automationのexact persisted IDを得られた場合だけupdate対象とする。name・時刻近接からIDを推測しない。lookup不能・複数一致・malformed entityは新規/更新を勝手に選ばずfail closedする。
- Codex appの`automation_update`を使い、現在のローカルタスクへ紐づくheartbeat automationを作成または更新する。standalone taskやworktree automationは使わない。
- automation名は`AUTO_RESUME_KEY`を使い、同名があれば新規作成せず更新する。
- 実行時刻は`AUTO_RESUME_AT_RFC3339`が表す絶対時刻とする。offsetを捨てずUTCへ変換し、時刻前の固定間隔pollingは行わない。
- heartbeat schedulerは`DTSTART`の`TZID`を`next_run_at`計算へ反映せず、壁時計部分をUTCとして扱う。`DTSTART;TZID=Asia/Tokyo`は使わない。
- 絶対時刻の指定は、`AUTO_RESUME_AT_RFC3339`をUTCへ変換し、UTCの年月日時分秒を`DTSTART:YYYYMMDDTHHMMSS`、繰り返しを`RRULE:FREQ=DAILY;COUNT=1`とする1回限りの予約として設定する。JSTやCSTの壁時計時刻をそのまま渡さない。
- automationの実行環境は`REPO_ROOT`と同じローカルcheckoutを選ぶ。別worktreeではrepo hashが変わりresume stateを参照できない。
- 生のautomation directiveやRRULEを本文へ出力せず、利用可能なtool schemaに従う。

### wake prompt contract

- heartbeat promptはtask/workflow specificationではなくwake triggerと非導出stateのdurable carrierだけにする。current repository authorityから再取得できるreview・validation・install・Plan同期・packet処理・authority再読手順をpromptへ複製しない。
- prompt本文は次の固定形を使う。`run_control`はユーザーが明示した停止境界・継続境界など、current repository authorityへlosslessに永続化されておらずwake後にも必要な場合だけ2行目へ原文のまま追加し、存在しない場合は行自体を省略する。

```text
GLM 5h auto-resume trigger. repo_root=<REPO_ROOT>; expected_task_id=<TASK_ID>.
run_control=<EXPLICIT_NON_DERIVABLE_USER_BOUNDARY>
```

- `AUTO_RESUME_KEY`・発火時刻・rruleはscheduler contractが既にdurableに保持するためpromptへ重複させない。thread identityも`CODEX_THREAD_ID`のcommand-boundary ownershipを使いpromptへ入れない。
- createとupdateでは同じcompact prompt contractを使う。schedule更新だけを理由にworkflow proseを追加しない。必要な`run_control`をlosslessに保持できない場合は勝手に要約・推測せず予約をfail closedする。
- wake promptはcurrent repository authorityより優先するauthorityではない。予約後にworkflow規則が更新されても古いprompt proseでshadowしない。

### 既存automationの更新

- 同名のheartbeat automationが既に存在する場合、そのautomation IDへ絶対時刻anchorの`DTSTART`と`RRULE:FREQ=DAILY;COUNT=1`を直接updateする。placeholderを作り直さない。

### 新規automationの二段階作成

- automationが存在しない場合、DTSTART付きの即時createはCodex appに`Immediate automation creates cannot include DTSTART`として拒否されるため、DTSTART付きの単一段階createは行わない。
- 第一段階として、DTSTARTなし・status PAUSEDの最小placeholder scheduleでautomationを作成する。placeholder scheduleは時刻帯や現在時刻によらず常にfuture occurrenceを持つ`RRULE:FREQ=HOURLY`を使う。特定の壁時計時刻に依存する選び方はしない。PAUSEDのためplaceholderがそのまま実行されることはない。
- `suggested_create`は候補カードの表示のみであり永続automationではない。第一段階でも`suggested_create`を呼ばない。
- 第一段階の成功応答に含まれるautomation IDだけを正確なIDとして採用する。成功前にIDを推測・仮定しない。
- 第二段階として、第一段階で得た同一IDへ絶対時刻anchorの`DTSTART`と`RRULE:FREQ=DAILY;COUNT=1`を設定し、statusをACTIVEへupdateする。
- 第一段階のcreate失敗を成功扱いしない。失敗応答・automation IDを含まない応答の場合は第二段階へ進まず、作成失敗として扱う。
- 第一段階成功後の第二段階update失敗の場合、作成済みplaceholder automationをbest-effortで削除し、PAUSEDの半端な予約を残さない。削除にも失敗した場合はそのautomation IDを手動fallback案内に明示する。
- 二段階作成の最終verify失敗もfail closedとする。予約済みと報告せず、作成済みautomationを削除または停止してから手動`glm-worker --resume`fallbackを明示する。

### automation応答の検査

- `automation_update`の応答はfield semanticsで構造的に検査する。応答全体を文字列化してfailure語のraw substring検査を行わない。content欄だけを読み、空や短い出力を成功扱いしない。過去にinvalid arguments文字列をcontentだけ読んで空出力と誤認し、作成失敗を見落とした実障害があり、2026-09-04には成功応答のfield名`isError`へのcase-insensitive `error` substring検査で成功を失敗へ誤判定し、PAUSED placeholderと追加round tripを残した。
- top-levelの`isError` fieldを機械的に読む。`isError:true`は作成・更新失敗とする。`isError`の有無・値が判定不能なmalformed/ambiguous responseも失敗とする。`isError:false`は成功の必要条件であって成功そのものではなく、`errorCount:0`等の否定・zero値やfield名だけをraw substringでfailure語扱いしない。
- `invalid`、`error`、`failed`は、content text内のmachine payload・message値として明示的に現れた場合だけ失敗とする。field名・否定値・zero値としての文字列出現を失敗根拠にしない。
- 候補成功は、明示的な作成・更新成功message、exact automation ID、応答が報告する期待mode/statusが全て揃った場合だけとする。期待ID・mode/statusの欠損または不一致、空文字列、`Rendered suggestion`、候補カード表示、malformed/ambiguous responseは作成失敗とする。automation IDをname・時刻・会話memoryから推測しない。候補成功は予約済みではなく、次項の実体検証を通る必要がある。
- 構造検査で候補成功となったcreate応答をfailure語の誤検出で失敗扱いにせず、同じorchestration内で第二段階update・実体検証・失敗時cleanupへそのまま継続する。誤判定によりACTIVE化前のPAUSED placeholderを残さない。
- `suggested_create`は候補カードの表示のみでありautomation作成完了ではない。`suggested_create`を呼ばない、予約成功や作成成功の根拠にしない。
- 応答検査で見落としても、次項の実体検証が未作成・row欠損・不一致をFAILとして検出する二段防御である。automation tool応答はCodexのtool境界にあり`glm-worker`へ直接渡らないため、postcondition検証が最終的なfail-closed手段となる。

### 候補成功後の実体検証

- 候補成功後、ただちに同じtool orchestration内で`REPO_ROOT`において`glm-worker --verify-auto-resume <AUTO_RESUME_KEY> <AUTO_RESUME_AT_RFC3339>`を実行する。このcommand自身が現在processの`CODEX_THREAD_ID`を読み、保存済みautomationのtarget threadと照合する。親Codexはthread IDを探索・copy・argvへ渡さない。
- この検証は保存済みautomation TOML実体(`~/.codex/automations/<key>/automation.toml`)のid・name・status ACTIVE・target_thread_id・rrule完全契約(UTC DTSTART + 改行 + `RRULE:FREQ=DAILY;COUNT=1`の正確な2行)と、`~/.codex/sqlite/codex-dev.db`の`automations.next_run_at`・id・status・rruleを期待ID・対象thread・status ACTIVE・絶対時刻で照合する。Codex appのSQLite automations表にthread_id列はなく、対象threadはTOMLのtarget_thread_idでのみ検証する。
- 検証成功(exit 0と`--verify-auto-resume`結果JSON)だけが予約成功の根拠となる。この時点で初めてrate limit停止を報告してよい。
- `CODEX_THREAD_ID`欠落・空・invalidによる`not_found`は対象threadを推測せずfail closedする。`env`全体のdump、`env | rg`、Desktop thread一覧、automation名、会話memory、task ID、時刻近接からcurrent threadを復元しない。
- 検証失敗(stderr error JSON `kind: verification_failed`、exit 1)の場合、予約済みと報告しない。検証理由からschema引数の誤りを特定し、引数を修正して`automation_update`のupdate(二段階作成の場合は第二段階)を同じorchestration内で再試行する。再試行は最大1回まで。再試行後も失敗の場合、新規作成分のautomationを削除または停止し、作成不能として手動`glm-worker --resume`fallbackを明示する。
- 検証不可(stderr error JSON `kind: verification_unavailable`、exit 1)の場合(sqlite3不在・DB/schema読取不能)、同じorchestration内のCodex app automation responseで同じautomation ID・対象task・次回実行時刻が意図したJST時刻と一致することを確認できた場合だけ予約成功とする。確認不能な場合は作成失敗とし、手動`glm-worker --resume`fallbackを明示する。

## wake時

本節はGLM resume automationの発火時と、coalesce判定でGLM automationを作らなかった場合にCodex 5h wakeの「作業を続けろ」で親実装taskが再開された場合の両方に適用する。automation削除・停止・更新は、実GLM resume automationを作成済みのときだけ行う。

wake promptはworkflow authorityとして読まない。commandを実行する前に、その時点のrepository authorityが要求するRules / Plan / exact ACTIVE taskを現在checkoutから再読し、generic workflowはそこからだけ取得する。promptから利用するのは`repo_root`・`expected_task_id`・存在する場合の`run_control`だけである。authority再読が終わった後のmachine state検証からresume terminal/handoff取得までは、1回のcode-mode/tool orchestration内で連続実行し、その間にSolへ戻らない。

1. 同じorchestrationの最初のnested tool callとして`REPO_ROOT`で`glm-worker --status`を実行し、出力を1 JSONとしてparseする。
2. 出力JSONの`task_id`がwake promptの`expected_task_id`と完全一致し、`task_status: rate-limited`、`resume_available: true`であることを機械確認する。expected task IDの正は予約時rate-limit packetの`detail.task_id`をcompact wake promptへ保存した値だけであり、automation名・会話memory・現在唯一のrate-limited task・時刻近接から復元しない。
3. task ID不一致、reset済み、rate limit以外のstatus、parse不能なら`glm-parent-action resume`を実行しない。実GLM resume automationを作成済みの場合は同じorchestration内で該当automationを削除または停止し、期待値と観測値だけのbounded mismatch resultを返して停止する。
4. 条件が一致した場合だけ、Solへ戻らず同じcheckoutで`glm-parent-action resume`をsandbox外実行し、`glm-execution.md`のblocking parent-action terminal/handoff contractに従って同じorchestration内でterminal resultまで待つ。standalone `glm-worker --resume`へ切り替えない。
5. 再びrate limit停止(error JSON `kind: rate_limited`)になった場合は、そのterminal detailを使って同じcode-mode flowでcoalesce判定から予約/更新/verifyまでやり直す。実GLM resume automationを作成済みの場合は、新しい`detail.auto_resume_at_rfc3339`で同じ`detail.auto_resume_key`のautomationを更新する。
6. `PASS`、`NEEDS_SOL_DECISION`、`NEEDS_SOL_REVIEW`、`WORKER_ERROR`のいずれかへ進んだら、実GLM resume automationを作成済みの場合は同じorchestration内でそれを削除または停止し、terminal + canonical handoffだけを親へ返して通常のGLM packet処理を継続する。

## 不変条件

- current parent thread identityの正は各auto-resume command processの`CODEX_THREAD_ID`だけとする。parentはthread ID取得のために`env`列挙・`env | rg`・Desktop `list_threads`等を実行せず、command引数へthread IDを手入力しない。identityが取得不能なら推測せず停止する。
- coalesce判定は`glm-worker --check-wake-coalesce`のmachine JSON `decision`だけを根拠にし、automation名・会話memory・親判断だけでGLM wakeを作る・作らないを決めない。coalesceしてもCodex wakeとGLM resumeの責務・session ownershipは統合せず、Codex 5h wake側の登録・再予約・検証契約(`codex-auto-resume.md`)は変更しない。
- deterministic scheduler stageの途中結果をSolへ返して次stageを選ばせない。semantic continuation intentが確定した後のcoalesce/lookup/create/update/verify、wake authority再読後のstatus/exact-match/resumeはそれぞれ1 parent tool orchestrationに畳む。
- 新しい`glm-worker "<元依頼>"`を起動しない。
- working tree、task state、worker/reviewer session、resume checkpointを破棄・resetしない。
- 元依頼やSol判断を再構成せず、保存済みcheckpointからだけ再開する。
- ユーザーが自動再開前に明示的に`--reset`した場合、古いautomationから再開しない。
