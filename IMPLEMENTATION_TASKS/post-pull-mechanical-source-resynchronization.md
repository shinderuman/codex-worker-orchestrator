# Task: Pull後のsource再同期を機械的に完結する

## Original instruction

2026-11-04 ユーザー指示：

```text
git pullした際に再同期するのは機械的にやってもいいんじゃないのか
```

## Amendments

none

## Resolved references

- [Audit #1503](https://github.com/shinderuman/codex-worker-orchestrator/issues/1503)：`git pull --ff-only`で `c8acd619→dc099fe3`。後続Task追加とPlan更新だけだったが、親Codexは再同期に必要な操作を探索し、`materialize`の投影overflowや複数の拒否を経てsource再束縛した。
- [Audit #1420](https://github.com/shinderuman/codex-worker-orchestrator/issues/1420)：Pull後に旧候補のまま検証が進み、遅い段階で拒否された前例。現行のController source continuity、`adopt-mutable`、rebind、suspend/materializeの正規ownerを再利用する。

## Purpose

許可済みPullによるGitのfast-forward後、Codexの操作探索や反復した手動調停なしに、Controllerとsourceの状態を正規経路へ同期する。

## External feasibility

status: not-applicable

## Contract

- Pull完了後のprimary/remoteの進行を検出し、Task authority、source、lease、snapshot、未commit変更、remote refの整合を既存Controllerで検証する。
- 許容されたfast-forwardは既存のtyped操作を使用して必要な再束縛・後続状態遷移を機械的に収束させる。未完了の処理、競合、実行中lease、部分失敗は正規の停止・復旧経路へ帰属させる。
- 親CodexのCLI探索、手動State編集、同じ状態に対する不要な操作の往復をなくす。Pull自体の自動実行や新たなGit更新権限は追加しない。
- **Review証拠の再利用判定は本Taskの範囲外**。現行のReview失効・再Review要求を保持し、source再同期の成立とReview同等性の成立を混同しない。

## Must not

- 既存のfail-closed、source/Task/lease権限、独立Review、Publicationの検証を省略しない。
- 第二Controller、汎用復旧framework、Git hookによる無条件なPullを作らない。

## Acceptance criteria

- Audit #1503のPull後の状態を実際のGit/Controller境界で再現し、既存のtyped経路だけでsource再同期を機械的に完了する。親の都度CLI探索・手動State修復を要しない。
- 正常な再同期後は現行規約どおりReviewが必要な状態へ進める。Review継承を実装しなくても単独で成立する。
- 非fast-forward、remote不整合、Task変更で不正なsource継承、未commit変更の衝突、stale/in-flight lease、途中失敗では誤った完了を許さず、合法的に停止・復旧する。
- Pull後の親model往復、コマンドの実処理時間、失敗・再試行をbefore/afterで比較する。待機時間を実処理に算入せず、変更差分と正負の検証証拠を残す。

## Historical invariants

- 1 Task 1 Commit、正規source/remote整合、既存の独立Reviewの保証を維持する。

## Dependencies

none
