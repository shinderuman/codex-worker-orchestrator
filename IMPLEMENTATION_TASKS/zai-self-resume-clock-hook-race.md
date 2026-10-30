# Task: 自己再開clock hookの並列データ競合を解消

## Original instruction

2026-10-29 ユーザー指示（原文）:

````text
Auditして
````

## Amendments

none

## Resolved references

- 今回のAudit: Dogfood Bundle `22dd61fb-68bb-4c99-bb4d-0289fe879924.zip`、ACTIVEだった`temporary-executable-etxtbsy.md`。新たに確認されたのは**ETXTBSYとは別原因**のGo race detector failure。実装要求は現行Gitと本Taskを正とし、外部管理Issueを正本にしない。
- 一次証拠: Bundle `attempt/artifacts/goquality-full-race.log`。`internal/app/zai_self_resume_test.go:replaceZaiSelfResumeNow`（`TestStatusRateLimitResumeMode`から呼出）がpackage-level `zaiSelfResumeNow`に書き込み、並行中の`internal/app/execution.go:executeZaiSelfResumeLoop`（`TestExecuteNewTaskReachesPass`経由）が同じ値を読んでdata race。`zai_self_resume_status_test.go:TestStatusRateLimitResumeMode`は`t.Parallel()`を使用。
- 同じsource digestの`goquality test -race ./...`でPASSとFAILが発生。FAILログは保存済み。PASS回は一時領域が回収されてrawログは未保存だがdiagnostic行が`attempt/artifacts/validation-summary.txt`に存在する。対象3ファイルはETXTBSY Task差分に含まれず既存のflaky race。再試行PASSを修正の証拠としない。

## Purpose

自己再開用のproduction clock関数値をテストがグローバルに差し替え、別テストの実行が同期なしに読む競合を解消する。正規Go race gateが同一sourceで不規則にFAILし、検証・Publicationの時間とCodex使用量を増やす原因を除去し、自己再開の意味契約を維持する。

## External feasibility

status: not-applicable

## Contract

- 当該raceログとcurrent Go sourceに基づき、`replaceZaiSelfResumeNow`とcleanup、production read、ほかのglobal clock/wait hook、並列実行を追跡し、実際のraceのread/write双方を特定する。
- テストごとのclock隔離／明示的な依存注入を優先して実装ownerを修正する。共有hookが不可避なら**読取りと書込みの双方**を同じ同期境界で保護し、Cleanup/並列ケースでもrace-freeにする。書込み側だけmutexを置く対症療法は不可。
- 既存のself-resume 5h reset、post-reset retry、initial grace、interrupt、durable stop、normal new Task実行のsemanticsを維持する。実際にproduction自体で同じdata raceが発生したとは断定しない。
- 既存owner/fixtureを再利用し、巨大なclock frameworkや別stateを増やさない。ETXTBSYとは別Taskとして独立して実装・評価する。

## Must not

- `go test -race`のスキップ、対象テスト除外、`-parallel=1`の強制、無関係な`t.Parallel()`の一律削除、sleep/timeout延長、assertion除去で解決扱いしない。
- たまたまraceが出なかったPASSだけで原因除去を宣言しない。production self-resume動作を無断変更しない。

## Acceptance criteria

- `TestStatusRateLimitResumeMode`と`TestExecuteNewTaskReachesPass`等が並行する再現条件で、以前のread/write raceが構造的に発生しないことを確認する。race detectorで繰返し検証し、実際の共有値/cleanupの同期・隔離をコードでも確認する。
- 対応packageのtest/raceと正規全体`go test ./...`および`go test -race ./...`を要求される実backendで実行し、既存の自己再開契約とQuality Deltaを維持する。未再現・他のflaky FAILは分けて記録する。
- 修正前後の同source/同条件のrace実行結果と追加検証の実時間を証拠化する。不要な全体再試行を機械的に増やさない。

## Historical invariants

- 正規Worker/Reviewer、Parent semantic authority、source/Task identityと1 Task 1 Commitを維持する。
- ETXTBSYとは別rootの管理、original requirementとcurrent Gitを正とする。

## Dependencies

none
