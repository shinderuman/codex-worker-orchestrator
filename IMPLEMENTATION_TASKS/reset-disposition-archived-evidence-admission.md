# Task: reset disposition archived evidence admission

## Original instruction

````text
今回の監査作業にこのExternal Reviewの範囲も入れてくれ
````

## Amendments

none

## Resolved references

- current reset implementationへのExternal Reviewは `glm-worker/internal/state/task_disposition.go` の `ValidateResetDispositionForNewTask` が `ArchivedTaskStatsEvidence(record.TaskID)` の `os.ErrNotExist` と `evidence.Proven == false` を拒否せず、新task admissionを成功させ得ると指摘した
- current main `4072e033940c2c2b02657cf3376bc082124987b0` でも、archive evidenceのstatus mismatchは `err == nil && evidence.Proven` の場合だけ拒否し、missing archiveはsuccess、unproven evidenceもsuccessとなる
- 直前のgeneric reset lifecycle実装でresetをexplicit disposition付きtransitionへ変更したが、本Findingはそのdisposition provenanceを次task admissionで検証する境界からescapeしたcorrectness defectである
- archive evidenceをlegacy recoveryで正規に再構成できる経路が既に存在する場合は、そのmachine recoveryをvalidation前に使用できる。再構成不能なmissing / malformed / unproven evidenceを成功へ縮退させてはならない

## Purpose

explicit reset disposition後のnew-task admissionを、旧taskのproven archived lifecycle/status evidenceへfail-closedに結び付け、archive欠落や証明不能状態でreset provenanceを暗黙に信頼して次taskを開始できないようにする。

## External feasibility

status: not-applicable

## Contract

- `ValidateResetDispositionForNewTask` はreset disposition recordだけでなく、対応するarchived task evidenceがmachine-verifiableに存在し `Proven == true` であることを要求する
- archived evidence lookupのmissingを含むerrorは、正規recoveryが成功してevidenceを再構成できない限りnew-task admissionを拒否する
- evidenceが取得できても `Proven == false` ならunknownをsuccess扱いせず拒否する
- proven evidence取得後に、recorded `FromStatus` とarchived statusの一致を検証する
- existing legacy recoveryがこのevidenceをlosslessに再構成できる場合は、validation前のbounded machine recoveryとして再利用し、第二のreset/archive authorityを作らない
- normal completed-task cleanup、explicit cancel / abandon / recovery、provider/rate recovery、user interruptionの既存合法経路を維持する

## Must not

- `os.ErrNotExist` を「古いstateだから問題ない」等の推測でsuccessにしない
- `Proven == false` のevidenceからstatusを補完推定しない
- missing archiveを通すためにdisposition recordだけを唯一のprovenanceへ昇格しない
- unfinished taskを便宜的にcompleteへ書き換えない
- parent proseによる確認をmachine evidenceの代替にしない
- unrelated task lifecycle / archive architectureを全面再設計しない

## Acceptance criteria

- reset disposition recordが存在しても対応archiveがmissingのfixtureでnew-task admissionがfail closedする
- archive evidenceが存在しても `Proven == false` のfixtureでnew-task admissionがfail closedする
- proven archive statusがrecorded `FromStatus` と不一致なら既存どおり拒否する
- proven archive statusが一致する合法dispositionではnew taskを開始できる
- legacy recoveryでarchive evidenceを正規再構成できるfixtureがある場合、そのrecovery後だけadmissionが成功する
- cancel / abandon / recoveryとnormal completionの既存合法transitionを壊さない
- Repository Lintと関連Go testがPASSする

## Historical invariants

- resetはunfinished lifecycleを消すescape hatchではなく、explicit dispositionとmachine-readable provenanceを持つtransitionである
- unknown / missing provenanceはsuccessへ縮退させない
- task lifecycle / archived stats / dispositionの既存canonical ownerを再利用し、第二state authorityを追加しない

## Dependencies

none
