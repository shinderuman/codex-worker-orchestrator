# Task: 設定可能なピーク時間帯停止を将来検討する

## Original instruction

````text
TASK

将来実装候補として、GLM Coding Planのquota節約を目的とした「設定可能なピーク時間帯停止」をBLOCKED taskとしてPlanへ追加する。

STATUS

- 現時点では実装しない。
- PoCもしない。
- ユーザーが明示的にGOを出すまで自動unblockしない。
- 現在ACTIVE taskを中断しない。
- NEXTへ昇格しない。

GOAL

GLM Coding Planのweekly quota導入・off-peak割引等を前提に、ユーザーが指定した時間帯はglm-workerによるGLM消費を停止し、時間帯終了後に安全に同じtaskを再開できるoptional featureを将来実装する。

重要:
この機能は完全opt-inとする。

設定なしでは現在と完全に同じ動作を維持する。

PRODUCT BEHAVIOR

概念:

通常実行
↓
configured pause window開始
↓
新規GLM callを開始しない
↓
実行中GLM callがあればsafe stop
↓
task / phase / session / worktree / checkpointを保持
↓
scheduled pause状態
↓
pause window終了
↓
既存resume機構を利用して同一taskを継続

単に「pause window内では次のcallを開始しない」だけでは不十分。

window開始時点ですでにworker / reviewer / auto-fix等のGLM callが実行中なら、quota消費を継続しないようsafe interruptionできる設計を検討する。

CONFIGURATION

- default OFF。
- repositoryごとの設定より、端末/account単位のlocal configurationを優先候補とする。
- provider quotaが複数repositoryで共有されるため、特定repoだけ停止して他repoがGLMを消費し続ける設計を標準にはしない。
- 現在のmodel / Claude接続先local overrideの思想は参考にする。
- ただしClaude child env用設定へ責務を混ぜず、glm-worker自身のlocal configurationとして分離する案を優先する。

候補例:

{
"pause_windows": [
{
"timezone": "Asia/Shanghai",
"days": ["mon", "tue", "wed", "thu", "fri"],
"start": "14:00",
"end": "18:00"
}
]
}

これは確定schemaではない。

PROVIDER POLICY

Z.aiの現在のpeak/off-peak仕様や時刻をproduction codeへhardcodeしない。

理由:
provider側のplan、weekly limit、peak曜日・時刻・割引条件は変更され得る。

実装着手時に最新のZ.ai公式仕様を再確認し、その時点の必要設定を決定する。

STOP STATE

既存の

- user interruption
- GLM 5h rate limit
- provider unavailable

とは意味を分離する。

scheduled pauseをuser interruptionとして流用して、停止理由を失わない。

候補:

scheduled_pause

状態から少なくとも、

- pause理由
- resume可能
- resume予定時刻
- 元task
- phase
- session

を機械的に確認できること。

COMMON ENFORCEMENT

prompt遵守へ依存させない。

worker / reviewer / auto-fix等のGLM invocationすべてが通る共通choke pointで、

- 現在GLM call開始可能か
- 次のpause境界はいつか

をdeterministicに判定する方向を優先する。

特定phaseだけpauseを回避できる構造にしない。

read-onlyでGLMを呼ばないstatus / telemetry / limit確認等まで無意味に停止させない。

RESUME

既存のresume checkpoint、`glm-worker --resume`、GLM 5h limit auto-resume等を最大限再利用する。

新しいdaemon / cron / polling subsystemを安易に追加しない。

pause終了後のwakeについては、既存one-shot resume scheduling contractへ一般化できるか検討する。

既存Codex 5h wake / GLM rate-limit wakeと時刻が重なる場合のcoalesceは将来検討対象とするが、初期実装へ無理に含めない。

POC REQUIRED BEFORE IMPLEMENTATION

本実装前に必ず以下をPoCする。

実行中GLM call
↓
時刻境界相当の自動safe stop
↓
checkpoint / task state / worktree / session保持確認
↓
一定時間後
↓
同一taskをresume
↓
worker / reviewer / auto-fixの元phaseから正常継続
↓
最終結果が非中断実行と意味的に同等

特に確認すること:

- call途中stopで成果物やworking treeが破損しない
- session resumeが成立する
- reviewer / auto-fix途中でも成立する
- stop直前・直後のrace
- pause開始とcall終了が競合した場合
- resume時刻に別のrate limit / provider unavailableが重なった場合
- process crash後もscheduled pause stateを復元できるか

NON-GOALS

- Z.ai現在仕様をコードへ固定する。
- defaultでピーク停止を有効にする。
- Weekly Limitそのものを推測・独自管理する。
- quota残量から自動的に利用量を最適化する。
- 新しい常駐daemonを作る。
- repository固有Plan lifecycleへ結合する。
- 本featureのためにglm-worker全体へscheduler責務を肥大化させる。

SUCCESS CRITERIA

将来実装時には、

- feature OFFなら既存behavior完全維持
- feature ONなら設定されたpause windowでGLM消費を開始・継続しない
- in-flight callも安全に停止可能
- task state / session / worktreeを失わない
- pause終了後に同じtaskを自動または既存resume経路で継続可能
- 複数repository利用時にもaccount-level policyとして一貫する
- provider固有時間帯変更は設定変更で追従できる
- Codex/Solへ不要な追加判断負荷を発生させない

ことを満たす。

BLOCKED CONDITION

以下を満たすまで着手禁止。

1. ユーザーが明示的にGOを出す。
2. その時点のZ.ai Coding Plan / weekly quota / peak-off-peak仕様を公式情報から再確認する。
3. 現行glm-workerのresume / stop / scheduler architectureを再確認する。
4. 本taskの設計をその時点の実装状況に合わせて再レビューする。

EXECUTION

現時点ではこの要求をlosslessなBLOCKED taskとして保存するだけ。

設計詳細確定、PoC、実装は行わない。
````

## Amendments

### 2026-09-10 user GO

````text
なおBLOCKEDも進められるなら進めてよい
````

この発言を本taskの明示GOとして扱う。他のBLOCKED taskについても、別のevidence/dependency条件まで解除したものとは解釈しない。

## Resolved references

- 「現在のmodel / Claude接続先local override」「既存resume機構」「既存one-shot resume scheduling contract」は、このtaskをunblockする時点のproduction実体を指す。
- 2026-09-10のZ.ai公式 `https://zcode.z.ai/en/docs/configuration` / `https://zcode.z.ai/en/docs/usage-stats` では、Coding Planに5-hour poolとweekly quotaがあり、off-peak reset-cardのwindowとusage thresholdはdynamic parameterで、アプリ表示を正とする旨が明記されている。したがってprovider時刻をproduction codeへhardcodeせずuser configurationとする原方針を維持する。
- current runtimeでは `glm-worker/internal/workflow/workflow.go` の `ModelRunner.Run` がworker / reviewer / auto-fixのmodel execution共通境界となり、`runner.StopController` がin-flight stopを扱う。`state.ResumeCheckpoint` / `ResumeStopKind` はrate-limit、provider-unavailable、user interruption等を区別して同一task/session resume stateを保持する。
- 設計再reviewでは、pause admissionをrepository Planやpromptへ入れずmodel-call共通境界へ置く。in-flight boundaryは既存StopControllerのprocess interruption機構を再利用可能性としてPoCするが、停止理由はuser interruptionへ潰さず `scheduled-pause` 相当の独立machine stateとする。
- account-level local configuration、default OFF、read-only non-GLM command非対象、既存resume/wake再利用、新daemon/cron/polling禁止を維持する。
- 以上によりBLOCKED CONDITION 1〜4は2026-09-10時点で満たした。production implementationへ進む前のlive PoC requirementは未充足であり、NEXT化後の最初の作業境界とする。

## Purpose

端末/account単位で設定したpause window中のGLM消費を、安全なin-flight停止と同一task再開を含めて抑制する完全opt-in機能を実装可能か、まず実経路PoCで確定する。

## Contract

- default OFFとし、設定なしの既存behaviorを維持する
- provider固有の時間帯やquota policyをproduction codeへhardcodeせず、端末/account単位のlocal configurationを第一候補とする
- worker、reviewer、auto-fixを含む全GLM invocationの共通choke pointで、call開始可否と次のpause境界をdeterministicに強制する
- pause開始時のin-flight callをsafe stopし、task、phase、session、worktree、checkpointを保持した別理由のmachine stateとして扱う
- pause終了後は既存checkpoint/resume/wake機構を最大限再利用し、新しいdaemon、cron、polling subsystemを追加しない
- production実装前に、worker/reviewer/auto-fix、境界race、他provider停止との重複、crash recoveryを含むlive PoCを実施し、失敗時はimplementationへ進まずBLOCKEDへ戻す

## Must not

- live PoC成功前にproduction featureを実装しない
- Z.aiの現行仕様や時刻を恒久codeへ固定しない
- scheduled pauseをuser interruptionやrate limitへ潰して停止理由を失わない
- prompt遵守だけでpauseを保証しない
- repository固有Plan lifecycle、Claude child env、read-only非GLM commandへ不要に責務を混ぜない
- quota残量最適化、常駐scheduler、glm-worker全体のscheduler化へscopeを広げない

## Acceptance criteria

- live PoCで実行中GLM callを時刻境界相当でsafe stopし、checkpoint / task state / worktree / sessionを保持できる
- worker / reviewer / auto-fixの各phaseで停止・resumeが成立し、非中断実行と意味的に同等の最終結果へ収束する
- stop直前/直後、call終了競合、rate limit/provider unavailable重複、process crash recoveryの代表境界を確認する
- PoC成功後にだけproduction Contractを最終化し、default OFF、account-level config、共通enforcement、独立stop reason、既存resume再利用をtestで固定する
- PoC失敗または外部制約判明時はfalse-completeせず、一次証拠付きでBLOCKEDへ戻す

## Historical invariants

- 最上位目的はSol High相当品質を可能な限り維持しながらCodex/Sol実消費を大幅に削減することであり、GLM quota節約はその目的を損なわないoptional optimizationとして扱う。
- wake schedule coalescingは既存のGLM 5h rate-limit resumeとCodex 5h wakeの重複発火を限定的に抑止するが、本taskのscheduled pause semanticsを実装済みとはみなさない。

## Dependencies

none

## Review findings

none

## Current boundary

BLOCKED CONDITION 1〜4は2026-09-10に解消。Plan NEXTへ昇格し、最初にlive safe-stop/resume PoCを行う。PoC成功前にproduction implementationへ進まない。
