# GLM rate limit自動再開

`glm-worker`がstderr error JSON(`{"error":{"kind":"rate_limited",...}}`、exit 1)の`detail.limit: ZAI_GLM_CODING_PLAN_5H`で停止した場合だけ適用する。

## 予約machine transaction

- error JSONの`detail.auto_resume_available: true`を確認し、同じtaskをreset後も継続するsemantic intentが確定したら、以降のcoalesce判定から保存実体verifyまでを1回のcode-mode/tool orchestrationで実行する。最初のnested tool callとして`REPO_ROOT`で`glm-worker --auto-resume-plan`を1回だけ実行する。ユーザーが明示した停止境界・継続境界など、current repository authorityへlosslessに永続化されておらずwake後にも必要なrun_controlだけ、`--run-control <原文>`で原文を渡す。存在しない場合はoption自体を省略し、要約・推測した文を渡さない。
- plan commandは現在processの`CODEX_THREAD_ID`をcanonicalな親thread identityとして読み、停止済みtask state(rate-limited stopと5h reset evidence)を正として、coalesce判定・既存automation lookup・expected automation ID/name・authorityを含むcompact prompt・PAUSED placeholder create spec・絶対時刻UTC update spec・verify引数を単一のstructured machine specとして生成する。親Codexはauthority文面・task ID・repo root・RFC3339 offset変換・schedule文字列・automation名・thread ID・promptを手入力・再構成しない。commandがusage errorで失敗した場合は引数を1回だけ修正して再実行し、それでも失敗すれば予約失敗として扱う。`CODEX_THREAD_ID`欠落・空・invalidによるfail closedはidentityを推測せず、予約へ進まない。
- machine outputの`expected_automation_id`はrate-limit packetの`detail.auto_resume_key`と同じ停止済みtask evidenceから同じ導出規則で機械生成される。親がIDを照合・推測・差し替えしない。
- machine outputの`status=coalesced`は、現在の親実装taskへ「作業を続けろ」を送るACTIVEなCodex 5h wakeを機械確認でき、その次回発火時刻がGLM resume時刻以降10分以内であることを意味する。この場合は同じorchestration内でGLM resume automationの作成・更新・削除を一切行わず、coalesceしたwake automation ID・次回発火時刻・追加待ち時間だけをbounded resultとして返してrate limit停止の報告へ進む。coalesce判定の結果だけで親判断や会話memoryによって予約を省略・復活させない。
- 許容時間境界は固定値でmachine側にも同じ値が固定される。coalesce条件は`GLM resume時刻 <= wake次回発火時刻 <= GLM resume時刻 + 10分`であり、下限はwakeがGLM reset前に発火してresumeを再びrate limit停止へ当てるのを防ぐ。上限10分は300分windowの3.3%・2026-08-26実測3分差の約3倍で、1 eventあたり親Codex turn 1回分の節約に対して許す追加待ち時間の上限である。この境界を自然言語の「近い」判断へ置き換えない。
- machine outputの`status=write_required`は「resume transaction relay」に従う。中間結果を`text()`等でSol-visible outputへ流して親modelへ戻らず、正常時は最終verifyまたはcoalesceのbounded postconditionだけを返す。

## resume transaction relay

- `--auto-resume-plan`の`status=write_required`では、`write` objectを変更せずそのままCodex appの`automation_update`へ渡す。親はautomation ID、name、target thread、status、RRULE、DTSTART、promptを補完・修正・再計算しない。利用可能なtool schemaに従い、生のautomation directiveやRRULEを本文へ出力しない。
- `write`内のpromptはmachine生成のauthority定型文を含む。この定型文は`IMPLEMENTATION_RULES.md`のrepository automation恒久許可、現在taskに既にある実装継続authority、GLMの禁止範囲がGit remote writeであってGLM実行全般ではないことのboundedな機械生成文であり、外部安全判定へはこの生成済みspecごと渡される。親がauthority文面を書き足し・省略しない。
- app toolのraw responseは内容を要約・整形・再解釈せず、同じplan outputの`token`とともに`glm-worker --auto-resume-response-stdin <payload-bytes> <token> [--sha256 <hex>]`へ渡す。stdin payloadは既存のbyte-counted stdin contractに従う。応答のfield semantics検査(top-levelの`isError`、exact automation ID、期待mode/status、explicit success message、`Rendered suggestion`や`suggested_create`の拒否、raw substring検査の禁止)はmachine validationが行い、親は応答を解釈しない。
- machineが次の`status=write_required`を返した場合だけ、そのoutputの`write`を同じ手順で1回実行する。retry回数、transaction identity、create後のexact returned ID、同一automationへのupdateはmachine stateを正とし、親で別transactionを組み立てない。lookup・create・update・verifyの間へcommentary・reasoning・通常assistant turnを挟まない。
- machineが`cleanup`を返した場合、best-effort cleanupはそのspecが指すexact automationだけへ実行する。automation名・時刻近接・一覧探索でcleanup対象を広げない。
- `status=verified`だけが予約成功である。検証の機械根拠は保存済みautomation TOML実体(`~/.codex/automations/<key>/automation.toml`)のid・name・status ACTIVE・target_thread_id・rrule完全契約(UTC DTSTART + 改行 + `RRULE:FREQ=DAILY;COUNT=1`)と、`~/.codex/sqlite/codex-dev.db`の`automations.next_run_at`・id・status・rruleが期待ID・対象thread・絶対時刻と一致することであり、transaction内部で現在processの`CODEX_THREAD_ID`による保存実体verifyとして実行される。この時点で初めてrate limit停止を報告してよい。
- `status=failed`、command error、malformed response、`isError:true`、保存実体`UNAVAILABLE`はfail closedとする。予約済みと報告せず、machineが返したcleanupだけを実行し、作成不能として手動`glm-worker --resume`fallbackを明示する。rate-limit window中の直接`--resume`はlifecycle admissionがreset時刻前に構造化fail closedする。timer・手動待機・直接resumeでwindowを短縮できず、手動fallbackはreset後のみ有効である。machine failed出力はcanonical `authority` objectと、`isError:true`拒否でmachine JSON payloadに拒否messageがある場合は制御文字を除去し長さ上限で切り詰めたboundedな拒否理由を`reason`へ含む。親はこのfailed出力自体を拒否理由とauthority scopeのbounded evidenceとして扱い、raw responseの別保持・再解釈で補わない。検証失敗後のupdate再試行はmachine transaction内部で最大1回だけ行われ、親が再試行を組み立てない。UI表示や時刻の目視一致で`verified`へ昇格させない。
- `write.boundary=external-unenforceable`は、repositoryからCodex app write自体を直接強制できない境界を表す。machine spec生成・response admission・保存実体postconditionまでをrepository側が所有し、親の役割は生成済みspecのfieldをlosslessにcreate/updateへ転送し、tool responseを次のmachine validationへ渡すことだけである。時刻・identity・成功判定を自然言語で補わない。
- 恒久許可を含む生成済みauthority scopeを渡してもなお外部安全判定に拒否された場合だけ、拒否理由と渡したauthority scopeはmachine failed出力のbounded evidence(boundedな`reason`拒否文とcanonical `authority` object)として残る。親はそれをそのまま使ってrepository内で修正可能な境界とCodex app側の外部修正境界を分離して報告する。automation安全審査を迂回しない。

## 生成されるmachine specの契約

- automation名とexpected IDは停止済みtask evidenceから機械決定される。同名automationの永続IDはmachine内部のlookupだけを正とし、name・時刻近接からIDを推測しない。lookup不能・entity不正はmachineがfail closedする。
- 実行時刻はreset時刻に固定graceを加えた絶対時刻で、offsetを捨てずUTCへ変換される。heartbeat schedulerは`DTSTART`の`TZID`を`next_run_at`計算へ反映せず壁時計部分をUTCとして扱うため、`DTSTART;TZID=...`は使われない。絶対時刻の指定はUTCの年月日時分秒を`DTSTART:YYYYMMDDTHHMMSS`、繰り返しを`RRULE:FREQ=DAILY;COUNT=1`とする1回限りの予約である。時刻前の固定間隔pollingは行わない。
- automationの実行環境は`REPO_ROOT`と同じローカルcheckoutを選ぶ。別worktreeではrepo hashが変わりresume stateを参照できない。
- automationが存在しない場合は、DTSTART付きの即時createがCodex appに`Immediate automation creates cannot include DTSTART`として拒否されるため、DTSTARTなし・status PAUSEDの`RRULE:FREQ=HOURLY` placeholder createと、第一段階で採用した同一IDへの絶対時刻anchor updateという二段階がmachine specとして返される。placeholder scheduleは時刻帯や現在時刻によらず常にfuture occurrenceを持つ。PAUSEDのためplaceholderがそのまま実行されることはない。第一段階の失敗を第二段階へ進めない。`suggested_create`は候補カードの表示のみであり永続automationではないため、呼ばない・成功の根拠にしない。
- prompt本文は`repo_root`・`expected_task_id`・authority定型文・存在する場合の`run_control`だけを含む固定形である。`AUTO_RESUME_KEY`・発火時刻・rruleはscheduler contractがdurableに保持するためpromptへ重複しない。thread identityも`CODEX_THREAD_ID`のcommand-boundary ownershipを使いpromptへ入れない。createとupdateで同じcompact promptを使い、schedule更新だけを理由にworkflow proseを追加しない。wake promptはcurrent repository authorityより優先するauthorityではなく、予約後にworkflow規則が更新されても古いprompt proseでshadowしない。
- `--verify-auto-resume`はread-only postcondition projectionとして単独でも実行できる。session stateやrepo-rootを書き込まず、sandbox内で実行可能である。予約成功判定はtransaction内部の検証を正とし、単独実行は再確認専用とする。

## wake時

本節はGLM resume automationの発火時と、coalesce判定でGLM automationを作らなかった場合にCodex 5h wakeの「作業を続けろ」で親実装taskが再開された場合の両方に適用する。automation削除・停止・更新は、実GLM resume automationを作成済みのときだけ行う。

wake promptはworkflow authorityとして読まない。commandを実行する前に、その時点のrepository authorityが要求するRules / Plan / exact ACTIVE taskを現在checkoutから再読し、generic workflowはそこからだけ取得する。promptから利用するのは`repo_root`・`expected_task_id`・存在する場合の`run_control`だけである。authority再読が終わった後のmachine state検証からresume terminal/handoff取得までは、1回のcode-mode/tool orchestration内で連続実行し、その間にSolへ戻らない。

1. 同じorchestrationの最初のnested tool callとして`REPO_ROOT`で`glm-worker --status`を実行し、出力を1 JSONとしてparseする。
2. 出力JSONの`task_id`がwake promptの`expected_task_id`と完全一致し、`task_status: rate-limited`、`resume_available: true`であることを機械確認する。expected task IDの正は予約時rate-limit evidenceをcompact wake promptへ保存した値だけであり、automation名・会話memory・現在唯一のrate-limited task・時刻近接から復元しない。
3. task ID不一致、reset済み、rate limit以外のstatus、parse不能なら`glm-parent-action resume`を実行しない。実GLM resume automationを作成済みの場合は同じorchestration内で該当automationを削除または停止し、期待値と観測値だけのbounded mismatch resultを返して停止する。
4. 条件が一致した場合だけ、Solへ戻らず同じcheckoutで`glm-parent-action resume`をsandbox外実行し、`glm-execution.md`のblocking parent-action terminal/handoff contractに従って同じorchestration内でterminal resultまで待つ。standalone `glm-worker --resume`へ切り替えない。
5. 再びrate limit停止(error JSON `kind: rate_limited`)になった場合は、そのterminal detailを停止済みstateとして、同じcode-mode flowで`--auto-resume-plan`から予約/更新/verifyまでやり直す。実GLM resume automationを作成済みの場合は、machine specが同じexpected automation IDへのupdateを返す。
6. `PASS`、`NEEDS_SOL_DECISION`、`NEEDS_SOL_REVIEW`、`WORKER_ERROR`のいずれかへ進んだら、実GLM resume automationを作成済みの場合は同じorchestration内でそれを削除または停止し、terminal + canonical handoffだけを親へ返して通常のGLM packet処理を継続する。

## 不変条件

- current parent thread identityの正は各auto-resume command processの`CODEX_THREAD_ID`だけとする。parentはthread ID取得のために`env`列挙・`env | rg`・Desktop `list_threads`等を実行せず、command引数へthread IDを手入力しない。identityが取得不能なら推測せず停止する。
- coalesce判定はmachine JSON `decision`だけを根拠にし、automation名・会話memory・親判断だけでGLM wakeを作る・作らないを決めない。coalesceしてもCodex wakeとGLM resumeの責務・session ownershipは統合せず、Codex 5h wake側の登録・再予約・検証契約(`codex-auto-resume.md`)は変更しない。
- deterministic scheduler stageの途中結果をSolへ返して次stageを選ばせない。semantic continuation intentが確定した後のplan/lookup/create/update/verify、wake authority再読後のstatus/exact-match/resumeはそれぞれ1 parent tool orchestrationに畳む。
- 新しい`glm-worker "<元依頼>"`を起動しない。
- working tree、task state、worker/reviewer session、resume checkpointを破棄・resetしない。
- 元依頼やSol判断を再構成せず、保存済みcheckpointからだけ再開する。
- ユーザーが自動再開前に明示的に`--reset`した場合、古いautomationから再開しない。
