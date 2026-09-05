# Task: Post-worker quality gate recovery

## Original instruction

````text
この件に限らず改善タスクは積んでいけって言ってるだろ
````

````text
あと機械化したものは自由言語での制限は薄くしていいんじゃないのか
すべて消すと機械で制限されている根拠を見失う可能性があるので全部消すわけにはいかない
だが記載を薄くすることでお前は他の判断を重要しすることになるかもしれない
なぜなら文字数が多ければ多いほどお前はルールを見落とすからだ

あとこの「課題を見つけたら課題化する」というのも機械化可能なら機械化すべきじゃないのか
````

## Amendments

none

## Resolved references

- 2026-09-05の隔離recovery taskはworkerがIMPLEMENTEDを返した後、pre-review harnesslintのquality tool version mismatchで親commandがworker_errorになった
- canonical handoffは`task_status=active`、`task_liveness=stale`、`required_action=none`、`allowed_actions=[]`、resume checkpointなしを返し、実装diffとworker sessionは存在するのに正規継続actionがなかった
- reset、新規task、standalone resumeはいずれも同一task/session・completed result保持contractを満たさないため使用していない

## Purpose

worker結果取得後のdeterministic gate失敗を、実装結果を失わず修復・再検証できるmachine lifecycle stateへ遷移させ、active/staleの行き止まりを防ぐ。

## External feasibility

status: not-applicable

## Contract

- IMPLEMENTED結果取得後にharnesslint等のpre-review deterministic gateが失敗した場合、completed worker result、session、phase、snapshot、gate failureをresume checkpointへ保存する
- task statusとhandoffを明示的なrecoverable stateへ遷移させ、環境または実装修正後の合法actionを一意に返す
- gateが環境precondition失敗なら同じworker model callを再実行せず、gate再検証後に保存済み結果から独立reviewへ進む
- gateが実装defectなら同一worker sessionへのfix経路へ進み、失敗原因と修正範囲を混同しない
- process終了後も`active/stale + required_action:none`をterminal recovery stateとして残さない

## Must not

- gate失敗をPASSまたはreview完了として扱わない
- completed worker resultを捨てて新規task/sessionへ暗黙再起動しない
- reset、state file手動削除、watch永続待機を正常復旧経路にしない
- 環境不一致をworker implementation defectとしてsemantic fixへ送らない

## Acceptance criteria

- worker IMPLEMENTED後のharnesslint environment failure、lint violation、harness内部errorを区別するintegration fixtureがある
- 各失敗でhandoffが`consistent:true`かつ一意のrequired/allowed recovery actionを返す
- environment修復後はworker再call 0回で保存済み結果からreviewへ進む
- implementation修正が必要な場合は同一worker sessionで修正し、独立reviewを再実行する
- task/process終了後に`active/stale + required_action:none`が残らないregression testがある
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- worker/reviewer sessionとcurrent implementation diffを復旧の正とし、新規sessionへ推測移行しない

## Dependencies

none

## Review findings

none

## Current boundary

decision resume blocking defectの復旧後に実行する。
