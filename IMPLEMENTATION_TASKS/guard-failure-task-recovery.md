# Task: guard failure後のtask-level recovery

## Original instruction

```text
じゃあそれも作業してくれ
今度こそPRでやるように
あとコミットはめちゃくちゃ細かい単位でやってくれ
以前の作業でお前のローカル環境が消えて作業が消えたことが何度もあった
コミットはPRをSquashMergeすればいい
```

## Amendments

none

## Resolved references

- 「それ」は、Git authority guard等の外側guard / infrastructure failure時にmodel sessionは破棄してもtask checkpointと既存artifact/resultを保持し、原因解消後にfresh sessionで同一taskを途中段階から回復できる経路を指す。
- 自動resume対象へ無条件追加するのではなく、guard failureは親による原因解消・現物確認後にtask-level recovery可能とする。
- 代表例は、worker result/artifact生成後のpost-call guard failureから、成果物の再利用可能性を検証して次のreview段階へ進めるケース。

## External feasibility

status: not-applicable

## Purpose

外側guard / infrastructure failureでmodel sessionを失効させた場合でも、完了済みmodel workを不必要に再実行せず、安全に同一taskを回復できるmachine recovery pathを提供する。

## Contract

- guard違反時のmodel session invalidationは維持する。
- task-level recoveryに必要なstage、result、snapshot、failure reasonをcheckpointとして保持する。
- recoveryはfresh model sessionを使用する。
- recovery前にrepository / parent-managed surface /保存成果の整合性をfail closedで確認する。
- worker resultが安全に再利用可能ならworker再実行を避け、後続review等の未完stageから継続できる。
- 通常の5h limit / provider unavailable / user interruption resume semanticsを壊さない。

## Must not

- guard failure後の同一model sessionを信用してresumeしない。
- 実際のprotected repository authority mutationを検出した状態を自動継続しない。
- 保存済みresult/artifactの存在だけで検証なしに成功扱いしない。
- genericな全failure自動resume機構へ拡張しない。
- mainへ直接pushしない。

## Acceptance criteria

- recoverableなpost-call guard failureがtask-level checkpointを残し、session identityは破棄される。
- 原因解消後、fresh sessionまたは保存済みresultから適切な未完stageへ回復できる。
- worker resultが完了済みで再利用可能なケースではworker model callを重複させずreviewへ進める。
- actual protected-repo mutation等のunsafe caseはrecoveryをfail closedする。
- existing rate-limit/provider/user-interrupt resume testsを維持する。
- focused tests、`go test ./...`、`go vet ./...`、Repository Lintを通す。
- 実装はPR経由とし、作業中commitは細粒度でremote branchへ保持する。最終mergeはSquash Merge可能な構成にする。

## Historical invariants

- GLM worker/reviewerはcommit/push authorityを持たない。
- guard違反時に汚染可能性のあるmodel sessionを再利用しない。
- parent Codex / GPT側の意味判断とGLM実装責務を混同しない。

## Dependencies

none

## Review findings

none

## Current boundary

GPT側の一時的なtool改善作業として実装中。Task 010の意味判断・成果物には変更を加えない。
