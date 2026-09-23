# Task: 総合レビュー一時成果物の終了時cleanup

## Original instruction

````text
今回のBundle内での作業時、証跡的なのをCodexが作っているが実装が終わったなら後でまとめて全部消したいと思っている
残す理由はあるだろうか
終わったタイミングで消さないと要らないデータが永久にリポジトリに残るのが気持ち悪い
````

````text
じゃあ今回のレビューが終わったら削除するタスクとしてIMPLEMENTATION_PLANに差し込むのがいいと思うんだがそれでいいか
削除はWeb GPTでもできるが、流れとしてはWeb GPTでの実装→Codexでの実装となるのでIssueに置くよりIMPLEMENTATION_PLANに置くほうが自然だからだ
実際にCodexでやるかWeb GPTでやるかはその時考える
削除忘れが生じないようにするためだ
````

## Amendments

none

## Resolved references

- `Review.md` の「Findingと実装・評価Taskの対応」に今回の総合レビュー由来16 finding・改善2件・評価3件の要求正本が列挙されている。
- `review-evidence/README.md` は同directoryをレビュー当時の再現証拠と定義し、fixtureは後続refactorへ自動追従するtest suiteではなく、実装開始時に必要な再現を各Taskの正規ownerへ移すとしている。
- current treeをhistorical archiveとして使わない。削除前の内容はGit history / CI / bundleから回収できる。

## Purpose

総合レビュー実装完了後に一時的なレビュー証拠・再現artifactをcurrent treeへ恒久残置せず、削除忘れを防ぐ。

## External feasibility

status: not-applicable

## Contract

- `Review.md` の対応表にある今回の総合レビュー由来Taskがすべて完了してから実行する。Web GPT / Codexのどちらが実行するかはこのTaskでは固定しない。
- cleanup開始時にcurrent Plan / Task / code / testから `review-evidence/` と `Review.md` への参照を再検索し、未完了要求の正本・再現入力として必要な参照が残っていないことを確認する。
- 各findingで恒久的に必要な再現条件・regression coverageは正規production testまたは正規ownerへ移っていることを確認する。レビュー時fixtureを互換目的で残さない。
- `review-evidence/` をdirectoryごと削除する。
- `Review.md` にcurrent requirement authorityを残さず、今回レビューの要求がTask / code / testへ移管済みであることを確認して削除する。live referenceがあれば正規ownerへ移してから削除する。
- 削除に伴うtracked Markdown参照切れ、lint違反、test fixture欠落を修正し、repository validationを通す。
- historical evidence保存のための別archive directory、コピー、要約ledgerを新設しない。必要時はGit history / CI / bundleを使用する。

## Must not

- 未完了Taskが再現に必要としているartifactを先に削除しない。
- regression test自体を「証跡cleanup」として削除してcoverageを落とさない。
- `Review.md` / `review-evidence/` の内容を別のtracked fileへ丸ごと移してcurrent treeへ残さない。
- old source shapeへfixtureを追従させるcompatibility layerを作らない。
- cleanupを理由にfindingの実装scopeや意味contractを変更しない。

## Acceptance criteria

- `Review.md`対応の今回レビュー由来Taskがすべて完了している。
- current unfinished Plan / Taskに`review-evidence/`または`Review.md`を要求authority・必須再現入力として使うものがない。
- 必要なregression coverageは正規test / owner側に存在する。
- current treeから`review-evidence/`と`Review.md`が削除され、不要な代替archiveを追加していない。
- repository lint / relevant tests / tracked Markdown consistencyがPASSする。
- deletion後のwhole diffを確認し、今回の総合レビュー以外のcurrent requirement・test・production artifactを削除していない。

## Historical invariants

- ordinary completion / historical reproduction evidenceはGit / CI / bundleから回収し、current repository treeを恒久archiveとして使わない。
- current schema / current ownerだけを保ち、レビュー時source shapeへの後方互換性を持たせない。

## Dependencies

- `Review.md` の「Findingと実装・評価Taskの対応」に列挙された16 finding・改善2件・評価3件がすべて完了していること。
