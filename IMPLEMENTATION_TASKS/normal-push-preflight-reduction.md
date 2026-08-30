# Task: normal fast-forward push前の冗長remote preflightを削減する

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
````

## Resolved references

- completed commentlint finalizationでは、通常publication前にsandbox外で`git fetch origin main`、`git rev-list --left-right --count HEAD...origin/main`、`git merge-base --is-ancestor origin/main HEAD`を実行し、Guardian approvalを消費した。
- その後、別のGuardian approvalで`git push origin main`を実行した。
- repository Git contractは通常pushをfast-forwardに限定し、force/non-fast-forwardを許可していない。
- normal `git push`自身がremote fast-forward admissionを行い、remoteが先行・divergeしていればremoteを変更せず拒否する。
- 既存parent finalization contractは、成功したnormal push自体をpublication evidenceとして扱い、push拒否・通信断等のambiguous stateだけ親判断へ戻す方向を既に採用している。

## External feasibility

status: not-applicable

## Purpose

通常のauthorized main publicationではpush自身をremote fast-forward admissionとして使い、毎回のfetch/ancestry preflightとGuardian/model往復を削減する。

## Contract

- `git.md`のcommit/push/ref authorizationを変更しない。
- force、force-with-lease、non-fast-forward、automatic merge/rebase/divergence repairを追加しない。
- local final HEAD/clean等の既存postconditionは維持する。
- authorized actionが通常`git push origin main`でremote inspectionに別目的が無い場合、unconditional fetch/rev-list/merge-base preflightを行わずnormal pushを実行する。
- pushがnon-fast-forward rejection、transport ambiguity、その他failureになった場合だけparent judgmentへ戻し、必要ならその時点でremote stateをfetch/inspectする。
- remote stateを別目的で必要とするworkflowまで一律にfetch禁止しない。
- model call/Guardian callを別箇所へ追加して相殺しない。

## Must not

- push rejectionを自動merge/rebaseでrepairしない。
- `--force`系optionへ切り替えない。
- remote-aheadをsuccess扱いしない。
- normal push成功後に同一成功を証明する追加fetch/statusを復活させない。

## Acceptance criteria

- representative clean authorized finalizationでremote publicationはpreflight fetch+pushではなくnormal push 1 actionになる。
- fast-forward fixtureは同じremote resultで成功する。
- remote-ahead/diverged fixtureはnormal pushがremote mutationなしで失敗し、parent recoveryへ戻る。
- force/non-fast-forward authorization boundaryを維持する。
- parent/Guardian normal-path finalization turnsが減る。
- full validationと独立reviewを通す。

## Historical invariants

- parent Codexだけがremote write authorityを持つ。
- normal publicationはfast-forwardである。
- ambiguous Git stateはparent judgmentへ戻す。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。parent finalization measurement/Acceptance自体は外部監査で行う。