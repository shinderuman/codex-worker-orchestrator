# Task: GPT external review PR #2をcurrent HEADへ照合する

## Original instruction

````text
EXTERNAL_REVIEW_INTAKE
range: a75795589343ce86b63960f3662d02b50895d9bf..e2765992c0ae596b6c566ea4189c4c283dd92287
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/2
branch: gpt-review/a757955-e276599
proposal_head: 71ab914e90c8d81d780ef50b48159925172e5147
この時点ではCodex自身によるコードレビュー・finding採否判断を行わない。
まずreview branchをGitHubから明示的にgit fetchする。ghによるPR本文参照だけで済ませない。
git fetch origin refs/heads/gpt-review/a757955-e276599:refs/remotes/origin/gpt-review/a757955-e276599
git cat-file -e '71ab914e90c8d81d780ef50b48159925172e5147^{commit}'
proposal_headがlocal Git objectとして参照可能であることを確認してからGLM dispatchへ進む。GPT branchのcheckout・merge・rebaseは不要。
Draft PRのレビュー本文とfetch済みreview branchの修正diffを取得し、全findingと修正proposalを間引かずGLMのreview/fix taskへlosslessに渡す。
GLMに現在HEADとの照合、finding成立性の検証、fetch済みGPT修正案の検証・必要な適応、成立する問題の修正、必要なtest実行を行わせる。
fetchまたはproposal_head確認に失敗した場合はGLM dispatchへ進まずtransport failureとして停止・報告する。
GPT branchをblind mergeしない。
GLM処理完了後は既存のCodex最終review / acceptanceフローへ戻る。
````

## Amendments

### 1

````text
外部レビューTaskより先にやってほしいやつというのが以下だ
なので止まってくれというのは解除だ
いまやってるやつが終わったらまず次にこれをやってその後後続タスクをつづけてくれ
````

### 2

````text
いや、上記タスクをPushまで進めたら一度止まってくれ
````

## Resolved references

- 「上記タスク」は本task、PR #2のexternal-review intakeを指す。
- 「外部レビューTask」は先に保存済みの`IMPLEMENTATION_TASKS/gpt-external-review-current-head-validation.md`（PR #1）を指す。本taskを先に完了し、PR #1は本taskのpush後まで未着手とする。
- fetch対象remote refは`refs/heads/gpt-review/a757955-e276599`、local tracking refは`refs/remotes/origin/gpt-review/a757955-e276599`、expected proposal commitは`71ab914e90c8d81d780ef50b48159925172e5147`である。
- PR本文・comment/reviewは`gh`、proposal diffはfetch済みlocal Git objectをauthorityとして取得する。PR本文だけでproposal diffを代替しない。

## External feasibility

status: not-applicable

## Purpose

GPT external review PR #2の全findingとfetch済みproposalをcurrent HEADへGLMが独立照合し、成立する問題だけを適応修正する。

## Contract

- 親CodexはGLM処理前にfindingの採否判断やコードレビューを行わない
- 指定refを明示fetchし、expected proposal commitがlocal Git objectとして参照可能なことを`git cat-file -e`で確認してからGLM dispatchする
- fetchまたはcommit確認失敗時はtransport failureとしてGLM dispatch前に停止する
- Draft PR本文・全comment/reviewと、fetch済みproposal commitの修正diffをlosslessにtaskへ追加してGLMへ渡す
- GLMは全findingをcurrent HEADで個別に成立/解消済み/false positiveへ分類し、proposal妥当性と副作用を一次証拠で検証する
- 成立findingだけをcurrent HEADへ適応修正し、必要なtestを実行する
- checkout・merge・rebase・blind patch適用を行わない
- GLM完了後は親Codex semantic review、acceptance、通常gateへ戻す
- 親Codexのfinal commitをremote mainへ通常fast-forward pushしたら停止し、PR #1や後続taskへ着手しない

## Must not

- `gh`によるPR本文取得だけでproposal transport成立扱いにしない
- findingやproposalを重要度・作業量・実装容易性で間引かない
- old reviewed rangeだけを見てcurrent HEADの後続変更を無視しない
- GPT proposalを正本としてcurrent source・contractより優先しない
- GPT branchをcheckout・merge・rebase・blind cherry-pickしない
- GLMにcommit/pushさせない
- 親CodexがGLM検証前に採否を先取りしない
- 本task push後にPR #1または後続taskを開始しない

## Acceptance criteria

- 指定fetch commandが成功し、expected proposal commitをlocal Git objectとして確認する
- PR本文・全comment/reviewとfetch済みproposal diffの全内容がlosslessにGLMへ渡る
- 全findingごとにcurrent HEADでの成立性、proposal妥当性、採用/適応/非採用理由が一次証拠付きで報告される
- 成立する問題だけがcurrent HEADへ修正され、proposal branchをblind mergeしていない
- 関連test・全必要gate・独立reviewを通し、親Codexが最終採否する
- 親commitとmainへの通常fast-forward push完了後に停止する

## Historical invariants

- reviewed range終端`e276599`以後のmain commitをreview済みと仮定せず、実行時current HEADへ照合する。
- GitHub transport確認はPR本文とfetch済みproposal commitの両方を必要とする。

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。quality-gate taskの同一commit同期・push・install確認後、指定refのfetchとproposal commit確認から開始する。
