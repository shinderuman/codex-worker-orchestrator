# Task: publication ref guard local Git scope

## Original instruction

````text
これは別セッションにやらせている開発ループのルールだ、これとリンク先を読んだうえでIMPLEMENTATION_PLAN類を更新してタスク化しろ
````

## Amendments

````text
そもそもタスクだけじゃなくてコードのどこに問題があるのか見たのか
````

````text
作業しろ
````

## Resolved references

- `.githooks/reference-transaction` は `prepared` phase の `refs/heads/*` 更新をすべて `glm-parent-action push-binding ref-guard` へ送っている。
- `glm-worker/internal/parentactioncmd/publication_git_guard.go` の `verifyPublicationRefUpdate` は、repository harness がactiveかつTask statusが `awaiting_parent_completion` または `complete` の場合、通常のbranch ref更新とpublication candidate promotionを区別せずcandidate検証へ進める。
- `verifyPublicationRefCandidate` は `oldOID == candidate.BaseHead && newOID == candidate.CommitOID` のexact promotion以外を拒否するため、通常の `git pull --ff-only` による `refs/heads/main` fast-forwardが `publication ref update rejected: only exact candidate promotion is admitted` で拒否された。
- 実観測では local `main=fd939d0...`, fetched/origin `main=e9e6930...`, clean working tree の通常fast-forwardがreference-transaction hookのprepared phaseでabortされた。
- current `publication_git_guard_test.go` はexact candidate promotion/rollbackのfail-closed性を検証するが、publication guard active中のordinary local Git ref mutationが許可されることを検証していない。

## Purpose

publication promotionのmachine guardを維持したまま、通常のローカルGit操作によるbranch ref mutationをpublication transactionと誤認して拒否するescaped correctness defectを修正する。

## Contract

- publication guardは、単に `refs/heads/*` が更新されたという事実だけをpublication authorization/provenanceとして扱わない。
- ordinary local Git operationsによるbranch ref更新と、glm-workerが所有する正規publication candidate promotionをmachine-observableな境界で区別する。
- harness activeかつTask statusが `awaiting_parent_completion` / `complete` でも、publicationではない通常のlocal branch updateを不当に拒否しない。
- 正規publication candidate promotionについては、exact candidate binding、branch identity、readiness、candidate commit validationの既存fail-closed contractを維持する。
- pre-push publication binding guardは弱めず、remote publication writeは従来どおりcandidate/binding authorityへ従う。
- `git pull`、`git commit`、`git merge` 等のoperation-name blacklist/allowlistで例外を積み上げず、publication transactionのauthority/provenanceを正として判定する。
- guard非active時の既存Git behaviorを変えない。

## Must not

- `refs/heads/main` だけを特例で除外して他branchの通常開発を壊したままにしない。
- `git pull` だけを特例許可してcommit/merge/rebase/branch作成等の同根ref mutationを残さない。
- exact candidate promotion以外もpublicationとして通すことでfail-closed性を弱めない。
- publication guard自体を無効化・恒久bypassして解決しない。
- Git command文字列や親のprose宣言だけを信頼境界にしない。

## Acceptance criteria

- repository harness activeかつpublication ref guard requiredなTask statusで、ordinary `git pull --ff-only` 相当のlocal branch fast-forwardがpublication candidate誤判定で拒否されない回帰testがある。
- ordinary local commit/branch ref updateの代表ケースがpublication guardによって誤拒否されないことをtestで固定する。
- exact candidate promotion以外のpublication mutationは引き続き拒否される。
- exact ready candidate promotionは引き続き許可される。
- invalid promoted candidateの既存rollback contractを維持する。
- pre-push publication bindingの既存negative/positive contractを維持する。
- implementationはoperation-nameの列挙ではなくpublication transaction authority/provenanceの境界としてreview可能である。
- repository標準validationと、hookを実際にinstallしたrepository fixture/smokeで通常local Git開発とpublication guardの両方を確認する。

## Historical invariants

- machine-enforced correctness guardは正当なowner境界だけをfail closedにし、無関係な通常開発操作を巻き込まない。
- publication candidate promotionとremote publication writeのbinding correctnessは弱めない。

## Dependencies

- `IMPLEMENTATION_TASKS/machine-enforced-control-authority-legitimacy.md`
