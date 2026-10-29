# Task: Git hookから別worktreeへの環境混入を防ぐ

## Original instruction

2026-10-29 親Codex採用要求:
中間評価のmetadata commitが、pre-commitからcanonical continuation guardを呼ぶときだけ失敗する。Git hookの環境とprimary/laneのGit入力を分離し、guardを有効なまま正規コミットを成立させる。評価Taskでは実装せず独立Taskへ起票する。

## Amendments

none

## Resolved references

- `.githooks/pre-commit`、`glm-worker/internal/parentactioncmd/continuation_gate.go:loadContinuationGateHandoff`、`controller.ParentContinuationConfig`。
- 2026-10-29: 通常commitは`git ls-files: exit status 128`で失敗し、guard単独は成功。
- live laneで`GIT_INDEX_FILE=.git/index git ls-files --error-unmatch IMPLEMENTATION_PLAN.local.md`は`Not a directory`、primary indexの絶対pathなら同commandは成功。絶対pathを指定したcommitはguard内で`execution workspace snapshot does not match live lease`となった。Git入力の混入は確認済みだが、関連する全環境変数・全経路の原因は実装Taskで確定する。

## Purpose

primaryのGit環境をlaneの証拠検査へ持ち込まず、通常Git hookから正規guardを実行できるようにする。

## External feasibility

status: not-applicable

## Contract

- hookが継承するGit環境と、primary/laneそれぞれで必要なrepository/index/worktreeを追跡する。
- 別repository/worktreeへ移る既存subprocess ownerで入力を適切に分離する。同じworktree内の正当なalternate indexは意図を保ち、無条件に消去しない。
- 実Git commit→pre-commit→guard→live lane snapshotの経路で検証する。guard単独のPASSで代用しない。
- 正当なindex/worktree変更、source mismatch、stale leaseは従来どおり拒否する。

## Must not

- hook無効化、no-verify、lease/state手動変更、誤ったsnapshotの採用で通さない。
- 全shellへ汎用環境消去wrapperを追加しない。親専用Taskの完了意味は別Taskで扱う。

## Acceptance criteria

- relative/absolute index、primary/lane、正当なalternate indexの各条件で参照先が意図したGit入力と一致する。
- 現在の再現commitがguardを通り、真のsource不一致は拒否される。
- 通常TaskのPublication/Ref guardとmetadata gateを維持し、独立した新stateを追加しない。

## Historical invariants

- 証拠と実際のGit入力を一致させ、機械的な拒否を偽装解除しない。

## Dependencies

none
