# Task: Git subprocess反復の削減

## Original instruction

ユーザー原文:

````text
Git呼び出し削減は課題として別途残してもいいとは思う
優先順は改善結果が出てから出いいと思う
````

## Amendments

## Resolved references

- 「Git呼び出し削減」は`macos-go-test-performance.md`の代表e2e計測から得た独立候補。代表testでGit起動10,458回、Worker子process28回が観測された。反復回数を実時間寄与率と同一視しない。
- 同じref名への`check-ref-format`反復と、同じobjectへの`cat-file -e`と`-t`の隣接起動が候補。guardのworktree/config/remote情報は可変であり、一括cacheを承認していない。
- 元Taskでこの最適化の実装は採用しない。Docker比較結果と準備・保守コストを踏まえて、親が後からpriorityを判断する。

## Purpose

Git subprocessの不要な反復を、権限・source・ref・object検証の意味を維持して減らせるか評価する。

## External feasibility

status: not-applicable

## Contract

- 着手前にcurrent sourceで反復と実時間寄与を確認し、Docker移行後にも残る費用対効果を親が判断する。
- 形式検査の結果再利用とobject検査の二重起動縮約を優先候補とし、意味とfailure provenanceを保つ。
- 実装前後の同条件でcommand数と所要時間を照合し、実測で寄与した変更だけを採用する。

## Must not

- guard、remote、worktree、HEAD/ref/current sourceの鮮度確認を一括cacheで省略しない。
- 新規daemon、第二state DB、旧schema互換fallbackを追加しない。
- 本Task登録だけで現在ACTIVEを切り替えたり実装を開始したりしない。

## Acceptance criteria

- 削減対象と維持する不変条件、失敗経路を根拠付きで示す。
- 採用時は関連test・独立Review・必要validationと同条件の性能比較を成立させる。
- Docker比較結果を踏まえたpriorityの親判断を経てから着手する。

## Historical invariants

- `IMPLEMENTATION_RULES.md`のsource authority、fail closed、品質契約と1 Task 1 Commitを維持する。

## Dependencies

none
