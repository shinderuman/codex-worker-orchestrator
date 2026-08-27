# Task: reviewer diff-first impact expansionと独立search

## Original instruction

````text
## Task 017: reviewer diff-first impact expansion + independent search

reviewerはまずdiff起点。

impact expansion後、必要な時だけ独立search。

worker search結果をそのまま信頼しない。
````

## Amendments

none

## Purpose

review tokenを抑えつつ影響範囲漏れと自己充足reviewを防ぐ。

## Contract

- diff-first、impact expansion、conditional independent searchをproduction prompt/dispatchへ配線
- worker query/resultとreviewer検証を分離記録

## Must not

- reviewer常時full search、worker結果の無検証採用をしない

## Acceptance criteria

- diff充分/不足、independent query、impact漏れscenario
- test、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- reviewer独立性、BM25 core
- 016 worker repo-search integrationはPR #22のSquash Merge commit `da468541683a832b102acecc60770678452e6fa4`で充足済み。worker searchはnavigation-onlyでありreviewer authorityではない

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。016 `worker-repo-search-integration`はPR #22のSquash Merge commit `da468541683a832b102acecc60770678452e6fa4`としてintegration済み。reviewerをdiff-firstで開始し、impact expansion後に必要な場合だけworker searchとは独立したrepo-searchを行う最小境界を調査・実装する。worker query/resultをreviewer authorityにせず、reviewer側の独立query/resultとtelemetryを分離して記録する。
