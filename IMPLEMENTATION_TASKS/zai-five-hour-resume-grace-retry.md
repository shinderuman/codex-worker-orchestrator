# Task: Z.ai 5h reset後のresume grace / retryを短縮・強化する

## Original instruction

```text
そう言えばいまってLimitから2分ぐらいバッファ持たせてResumeしてるんだっけ？
2分も待つ必要あるか？
数秒のバッファであとは10秒リトライとかそういうのでいいんじゃないの？
```

```text
そもそも2分待ってダメだったときはどうなってるの？
```

```text
じゃあIMPLEMENTATION_PLANに新しいタスクとして追加しておいてくれ
もちろんPRを作れ
Squash Merge時にコミットコメントはちゃんと書け
タスクの順序は最低でもSystem-oneよりはあとだ
優先順位はお前が決めていい
だが別になくても困らない機能なのでだいぶ優先度は低いと思う
```

## Amendments

- 2026-09-27 priority correction:

```text
いや022の前はおかしいだろ
```

```text
せめて105の前だろ
```

## Resolved references

- 現行 `glm-worker/internal/runner/zai_limit.go` は `autoResumeGrace = 2 * time.Minute` とし、providerが返した5h reset boundaryへ固定2分を加えてself-resume予定時刻を算出する。
- 現行 `glm-worker/internal/app/execution.go` のself-resume loopは、予定時刻にcheckpointを検証してresumeし、再度 `ZaiRateLimitError` を得ると同じloopへ戻る。ただし同じ既過去reset boundaryを再取得した場合、算出されたresume予定時刻が未来でなくなり、`five-hour self-resume reset boundary is not in the future` で自動継続が終了し得る。
- ユーザー提案の「数秒のバッファ」「10秒リトライ」は、reset反映遅延を固定2分待機で吸収するのではなく、短い初期待機と既知5h-limitに限定した短周期retryで吸収する方向を指す。
- 本改善はSystem-Oneより後に実施する低優先度改善であり、現在のSystem-One作業を割り込ませない。
- `105-session-rotation.md` は既にfulfilledで現行NEXTには存在しないため、最新priority correctionは、105後の最終reevaluationである `post-105-codex-efficiency-reevaluation.md` より前へ本taskを置き、022直前まで先送りしないscheduleとして反映する。

## Purpose

Z.ai 5h limit解除後のself-resumeで固定2分を常に待つ遅延を減らしつつ、reset boundary通過直後にprovider側反映が遅れた場合でも自動継続が一発失敗で止まらないようにする。

## Contract

- providerが返したcanonical `ResetAtRFC3339` をreset authorityとして維持し、reset時刻そのものを推測・書換えしない。
- 現行の固定2分graceを、reset直後の反映遅延だけを吸収する短い初期graceへ縮小する。具体値は単一のdeterministic constantとして実装・testで固定し、2分級の常時待機へ戻さない。
- 初期resume後も同じ既知Z.ai 5h limitが返り、canonical reset boundaryが既に通過済みの場合は、約10秒単位の短周期retryへ移行して同一checkpointから継続する。
- providerが新しい未来のcanonical reset boundaryを返した場合は、その新boundaryを正として通常の5h待機へ戻す。
- retry対象は既知のZ.ai 5h-limit classificationに限定し、任意の429、provider-unavailable、unknown provider failure、guard failure、validation failure等を同じretryへ混ぜない。
- retry中も既存Task identity、resume checkpoint、phase、session/review/auto-fix stateを維持し、fresh taskやfresh sessionへ暗黙fallbackしない。
- stop endpoint / user interruptionを尊重し、retry待機中でも正規停止できる既存control semanticsを維持する。
- rate-limit state / telemetry / status projectionから、initial grace待機とpost-reset retryの区別を追跡できること。不要な高頻度model callやbusy loopを作らない。

## Must not

- 5h limit以外の一般429やunknown failureを無期限retryするgeneric retryへ拡張しない。
- reset boundaryを現在時刻へ丸めたり、provider evidenceなしに「もう解除済み」とみなしたりしない。
- retryのためにcheckpoint、Task、review state、auto-fix lineageを破棄・再生成しない。
- retry間隔を0またはbusy-loop相当にしない。
- 固定2分waitを残したまま追加retryだけを足して、通常復帰latencyを改善しない実装にしない。
- System-One実行中の割り込みtaskとしてACTIVE化しない。

## Acceptance criteria

- canonical reset boundary通過後の通常self-resumeが、現行2分固定graceより明確に短い初期graceで開始する。
- reset直後に同じ5h-limitが返るケースでself-resumeがterminal終了せず、短周期retryから成功へ進める。
- 新しい未来reset boundaryが返るケースでは新boundaryに従って待機し、過去boundaryに対する短周期retryと混同しない。
- unrelated 429 / provider failure / unknown failureは5h post-reset retryへ入らない。
- retry中のstop requestが既存の正規interrupt経路で停止できる。
- Task/checkpoint/session/review/auto-fix continuityがretry前後で保持される。
- relevant unit / workflow testsで、初期grace、同一既過去boundary retry、新未来boundary、非対象failure、stop requestを検証する。
- repository lint、relevant Go tests、whole diff reviewがPASSする。

## Historical invariants

- providerが返したcanonical reset boundaryをauthorityとして使い、自然言語やwall-clock推測をprovider stateの代替にしない。
- 5h recoveryは同一Task / checkpointの継続であり、新規Task開始ではない。
- recovery latency短縮のためにfailure classification boundaryを広げない。

## Dependencies

- `IMPLEMENTATION_TASKS/system-one-review-feedback-eval.md`

## External feasibility

status: not-applicable
