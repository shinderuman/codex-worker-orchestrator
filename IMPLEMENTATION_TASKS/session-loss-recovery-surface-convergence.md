# Task: session-loss recovery surface convergence

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- formal DogfoodでWeekly Limit / parent session復帰後、canonical lost-main-tool recoveryの`glm-parent-action wait`を使わず、`glm-worker --status` → `glm-worker --watch` → `glm-worker --handoff`へ逸脱した
- `--status` / `--handoff`はduplicate parent projectionで失敗し、`--watch`はtask開始時からのhistorical event logを大量にparent contextへreplayした。最終的には`glm-parent-action review-evidence`から正しいmachine evidenceを取得した
- closed #436 / #456 / #458 / #459 / #461でdetached recovery mechanicsは`glm-parent-action wait`へ既に収束済みであり、同mechanismを再実装する必要はない
- current parent instructionはstatus/watch loopへの切替を禁止している一方、`glm-worker --watch` / ModeWatch / dedicated streaming exceptionはCLI surfaceとして残り、canonical Codex workflow上のcurrent consumerは確認できない
- human/operator用`glm-watcher`はrepository Codex workflowとは別責務であり、本taskへ含めない

## Purpose

parent tool/session loss時のCodex recovery entrypointを既存canonical surfaceへ収束させ、obsoleteなCodex-facing watch surfaceが誤選択されてhistorical replay/pollingへ退行する経路を除去する。

## External feasibility

status: not-applicable

## Contract

- primary parent tool/sessionを失ったactive task recoveryは既存`glm-parent-action wait`をsole canonical detach/recovery entrypointとして維持する
- terminal transport/parse failureは既存bounded `glm-worker --handoff recovery`を維持し、detach recoveryと混同しない
- current repository/Codex workflowにconsumerがないことをcurrent source・instructions・testsで再確認したうえで、`glm-worker --watch`、ModeWatch、watch専用streaming machine-output exception等のobsolete Codex-facing surfaceを削除する
- `--status`等の合法なread-only inspectionを全面禁止せず、diagnostic surfaceとcanonical recovery actionを区別する
- session-loss recoveryのmachine resultはtask/owner identityを保持し、既存#458/#459/#461のduplicate waiter / epoch / owner-lost invariantsを維持する
- user/operator monitoring responsibilityをproduction Codex recovery contractへ混ぜない

## Must not

- `glm-parent-action wait`と並ぶ新しいpoll/recovery state machineを作らない
- elapsed-time polling、status/watch loop、historical replayをcanonical recoveryへ戻さない
- terminal transport/parse recoveryから`--handoff recovery`を削除しない
- read-only diagnostics全般を削除してoperabilityを失わない
- human `glm-watcher`の実装・通知・tail behaviorを本taskのconsumer根拠または変更対象にしない

## Acceptance criteria

- parent tool/session loss fixtureが`glm-parent-action wait`から一度だけcanonical recoveryし、status/watch/handoff探索loopを必要としない
- normal Codex parent workflowのcommand/projection surfaceに`glm-worker --watch`が存在せず、historical event replayを誤選択できない
- current consumer scanでwatch削除により壊れるrepository-owned Codex pathがないことを確認する
- terminal transport/parse failureは`glm-worker --handoff recovery`で従来どおりbounded recoveryできる
- duplicate waiter / stale ownership epoch / owner-lost active taskのexisting regression testsを維持する
- machine JSON contract、Repository Lint、関連Go test/full suiteがPASSする

## Historical invariants

- detach recovery ownershipは`glm-parent-action wait`へ集約済みである
- lost tool/sessionを理由に同一taskをnew taskとして再startしない
- diagnosis surfaceとlegal next-action authorityを混同しない

## Dependencies

none
