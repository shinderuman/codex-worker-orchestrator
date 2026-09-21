# Task: skip Web GPT branch creation CI

## Original instruction

```text
じゃあまたこれを現在の次のタスクとして積んでおいてくれ
```

## Amendments

none

## Resolved references

- 「これ」は、`web-gpt/**` branchをmain HEADから新規作成しただけのpush eventでも `.github/workflows/ci.yml` のvalidationが起動し、mainと同一commit SHAへbranch側のcancelled / failed runが紐づいたfindingを指す。
- current CIは `web-gpt/**` pushをWeb GPTのremote validation入口として使うが、branch creation pushと実commit pushを区別していない。
- 旧 `web-gpt-validation.yml` には `github.event.created == false` によるbranch creation除外があったが、workflow再編でそのguardが失われた。
- branch creation時には新しいcommit差分がないためvalidation evidenceは増えず、main commitのcheck表示をbranch側runで汚すだけになる。

## Purpose

Web GPT branchの新規作成だけでCI validationを実行しないようにし、最初の実commit以降のpushでは既存のremote validation coverageを維持する。

## Contract

- `web-gpt/**` branch creation pushではbasic quality / full Go test / lint aggregator / install smokeを実行しない。
- branch作成後の実commit pushでは、現行どおりbasic quality / full Go test / lint aggregator / install smokeを実行する。
- Web GPT branchのpull_request eventを実validationしない現行の二重実行回避は維持する。
- 通常PR、main push、workflow_dispatchのvalidation behaviorは変更しない。
- branch creation除外はworkflow levelまたはjob条件として一貫して適用し、`basic`だけskipした結果`lint` aggregatorがfailureになるような部分的skipを作らない。
- 同種の回帰をmachine test / wiring checkで固定できる場合は最小scopeで追加する。

## Must not

- `web-gpt/**` の実commit pushに必要なremote validationを削除しない。
- main pushや通常PRのrequired validationを弱めない。
- branch creation runを単にfailureからsuccessへ見せかけるだけで、不要なrunner実行を残さない。
- CI trigger整理以外のunrelated workflow cleanupを混ぜない。

## Acceptance criteria

- main HEADを指す新規 `web-gpt/**` branchを作成しただけではvalidation runnerが起動しない、または全validationが意図的にskipされ、main commitへcancelled / failed validation runを残さない。
- そのbranchへ最初の実commitをpushするとbasic quality / full Go test / lint aggregator / install smokeが従来どおり実行される。
- Web GPT branchのsame-head pull_request eventで実validationが二重実行されない。
- 通常PR、main push、workflow_dispatchの既存validation coverageが維持される。
- Repository Lintと関連CI regression testがPASSする。

## Historical invariants

- Web GPTはローカルvalidation環境を持たないため、実commit push時のGitHub Actions remote validation経路を維持する。
- 同一headに対する不要なvalidation二重実行と、evidenceを増やさないrunner消費を避ける。

## Dependencies

none
