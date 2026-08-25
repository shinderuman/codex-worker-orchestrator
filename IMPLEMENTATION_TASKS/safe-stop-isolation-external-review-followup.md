# safe-stop / isolation external-review follow-up

## Original instruction

````text
b6442c2..6e2cdeb に対する外部static review follow-up。

公開HEAD 6e2cdebのproduction codeを確認した結果、以下4 findingがある。
記述を盲信せず、現在の実コード・ACTIVE/完了Task contract・既存testを一次証拠として各findingの成立性を再確認し、成立するものだけ修正すること。
成立しないfindingは無理に修正せず、反証となる具体的code path/test evidenceを残すこと。

## Finding 1 — P1

safe-stopがprocess group残存を検出してもauthoritative interrupted successを返し得る。

`runner/process_group_unix.go` の terminateProcessGroup は、TERM→KILL後にもgroupが残ればCleanupWarningを返す。
`runner.runCommand` はそれを InterruptedCallError.CleanupWarning に載せるだけでinterrupted経路へ進む。
`workflow.persistInterruptedStop` はCleanupWarningをoutcomeへ反映する前に
`stop.NotifyInterrupted(taskID)` を実行する。
stop endpointはInterrupted outcomeを
result=interrupted / task_status=interrupted / resume_available=true
として返すため、既知の残存processがある状態でも親Codexへ「安全停止済み」と通知できる。

safe-stop contractはgroup非残存を確認した後のmachine ackを要求しているため、これはcontract違反。

修正では、

- 残存process groupが観測された場合にsafe interrupted success authorityを発行しない
- checkpoint/recovery情報を保存する必要があるなら、安全停止ackとは分離する
- 親Codexが「安全にpreempt可能」と誤認しないtyped machine outcomeにする
- 単なるtimeout延長だけで修正扱いにしない
- cleanup residualを決定論的に注入/再現し、interrupted-success ackが出ないtestを追加する

こと。

## Finding 2 — P2

isolation branchを正常統合した後なら、別経路の非parent source commitまでresume gateが許可する。

`verifyIsolationIntegration` はisolation branch tipがcurrent HEADのancestorであることを確認するが、
tipからcurrent HEADまでに追加された変更を検証しない。

したがって、

stop
→ isolation branch作成
→ 正常統合
→ unrelated source commit
→ 元task resume

でもbranch tipはcurrent HEADのancestorなのでgateを通過できる。

実装コメント/contract上、HEAD前進の承認経路はparent-managed metadataと検証済みisolation integrationに限定されているため、それ以外のpost-integration source deltaを許可しないこと。

最低限、

- valid isolation integration後にdirect non-parent commitを追加 → fail closed
- valid isolation integration後にparent-managed metadataだけ追加 → pass

をproduction-path testで固定すること。

## Finding 3 — P2

interrupted retentionのdirty identityがGit mode/type/multi-stage indexを失っている。

StopDirtyFileはPath / IndexSHA / WorktreeSHAだけを保持する。
indexBlobHashesは`git ls-files -s`から最初のblob SHAだけを採用し、modeとstage、同pathの追加stage entryを捨てる。
worktreeContentHashもregular file内容またはsymlink targetだけをhashし、file type discriminatorやmodeを含めない。

そのため既にdirtyなfileについて、

- executable bitだけ変更
- regular/symlink typeだけ変更してhash inputが一致
- conflict indexのstage 2/3等だけ変更
  などを保持一致として見逃し得る。

既存GitSnapshotはfull `git ls-files -s`をdigest化してmode/SHA/stageを識別しているが、
interrupted retentionではparent-managed metadata例外があるためfull snapshotを雑に比較して修正しないこと。

non-parent保持対象について必要なGit identityをlosslessに比較し、
parent metadata更新を許可する既存semanticsは維持する。

少なくともchmod/type/multi-stage driftのregression testを追加すること。

## Finding 4 — P3

stop endpoint Closeがin-flight handler完了を待たないため、
natural completionとstop requestのraceでauthoritative ackを失い得る。

acceptLoopは各connectionをgoroutineでhandleするがhandler lifecycleを追跡していない。
CloseはNotifyFinished→listener close→accept loop終了待ち→socket削除だけで、
NotifyFinishedによって起床した既存handlerがresponseを書き終えるのを待たない。
Execute return後はprocess自体が終了可能なのでrequesterがEOF/stale failureになるraceがある。

active handlerを安全に追跡し、
Close時に既受理requestへのterminal/exited ack送信完了を保証する最小shutdown lifecycleを設計すること。
WaitGroup等を使う場合もAdd/Wait raceやdeadlockを作らないこと。

既存testのようにcontrollerへ直接NotifyFinishedするだけではなく、
pending requesterが存在する状態で実際にserver.Closeを実行し、
ackが確実に返るregression testを追加すること。

## 原因切り分け

Finding 1:
親contractはgroup非残存後のmachine ackを明示している。
implementation + GLM自己review / independent review / Sol / parent final gateのescaped defectとして扱う。

Finding 2:
承認済みHEAD movementは実装コメントにも明記済み。
implementationのprovenance closure不足とtest/review gap。

Finding 3:
上位contractのworking tree/state保持は存在するがmode/stage edge caseは明示度が低かった。
ただしlossy fingerprint設計とreview gapが主因。
必要ならcontract/testをこのsemanticまで明示化する。

Finding 4:
stop race/machine ackを扱う設計に対し、controller-level testだけでserver lifecycle shutdownまで閉じていなかったimplementation/test/review gap。

## 作業境界

- 4 findingをまず現在HEADで再検証する
- false positiveなら修正しない
- 成立findingは既存Task/Plan lifecycleへ正しく戻して修正する
- GLMへ実装を委譲してよいが、GLMにはcommit/pushさせない
- 親Codexが最終semantic reviewを行い、必要なSol gateを通す
- 関連testだけでなくrepository既存contractに従う全test/race/vet/build/gofmtを実施する
- unrelated refactor、generic control plane、compatibility layerを追加しない
- Greptile日次scheduled reviewの別件contractをこの修正へ混ぜない
- 親Codexのcommit/pushは現在のrepository/project instruction lifecycleに従うこと
````

## Amendments

none

## Resolved references

- review範囲`b6442c2..6e2cdeb`後にもsafe-stop/isolation周辺のcommitが存在するため、修正判断はACTIVE化時のcurrent HEADと関連task contractを正に再検証する
- 「外部static review」はGreptile日次scheduled reviewとは別のユーザー提供reviewである

## External feasibility

status: not-applicable

## Purpose

safe-stopのauthoritative ack、isolation統合後HEAD provenance、interrupted dirty identity、stop endpoint shutdown lifecycleについて、外部findingの成立性をcurrent production codeで再検証し、有効な欠陥だけを修正する。

## Contract

- 4 findingをcode path・state transition・production-path testで個別に成立/不成立判定する
- 成立findingは既存safe-stop/isolation contractへ最小修正で収束させる
- 不成立findingは具体的な反証code path/test evidenceを結果へ残す
- 原因層はユーザー指定の切り分けを一次証拠と照合し、GLMだけへ帰属させない

## Must not

- finding本文を検証せず実装前提にしない
- Greptile日次scheduled reviewを変更しない
- unrelated refactor、generic control plane、compatibility layerを追加しない
- 現ACTIVEのexternal feasibility dispatch gate未コミットdiffへ混ぜない
- GLMにcommit/pushさせない

## Acceptance criteria

- 4 findingごとの成立/不成立と一次証拠が明示される
- Finding 1成立時はcleanup residualでinterrupted-success authorityを発行しないtyped outcomeと決定論的testがある
- Finding 2成立時はisolation統合後の非parent source commitを拒否しparent metadata-only前進を許可するproduction-path testがある
- Finding 3成立時はnon-parent dirty identityがmode/type/multi-stage indexをlosslessに識別し既存parent metadata例外を維持する
- Finding 4成立時はpending requester中のserver.Closeでもterminal/exited ack送信完了を保証するtestがある
- 関連test、全test/race/vet/build/gofmt、独立review、必要なSol gate、親Codex final semantic reviewを完了する

## Historical invariants

- safe-stopのsuccess authorityはprocess group非残存確認後だけ発行する
- isolationによるHEAD前進は検証済みbranch統合とparent-managed metadataだけを許可する
- interrupted taskのworking tree・state・session・checkpointを失わない
- 最上位目的はSol High相当品質を維持しながらCodex/Sol消費を削減すること

## Dependencies

none

## Current boundary

external feasibility dispatch gateはproduction実装・review・Sol採否・commit・本配置まで完了し、本follow-upがACTIVEへ昇格した。4 findingの成立性再検証から開始し、前taskの実装へ混ぜない。
