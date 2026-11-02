# Task: 通常Task admissionのharness適用範囲

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
通常Task入口がharness inactiveの他repositoryにもcommitted Planを必須にする不整合を修正する。repository固有guardとgeneric executionの境界を親判断で確定する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1487#issuecomment-6588769975

````text
Codexの設計採否と引継ぎ方針です。確認対象はhead `7815dbdd034b37216616a7edec8bdd8d46370b24`、基点は `e3dcec1525adbee32e33078928510c94dcdbb404` です。

まず作業分担を明確にします。**Web GPTだけで本Taskを完成させることは求めません。ある程度実装・検証したDraftをCodexが引き取り、残りの設計接続、追加修正、検証、最終採否、Metadata同期まで行います。** 以下の判断に合わせて先行実装を整理し、未完部分を正確に残してください。

### 判断1：markerもPlanもない通常Git repository

**非harnessの通常Git利用は対応対象とします。README-only repositoryを「canonical authority未定義」で拒否することを、最終仕様・本Taskの修正完了とは認めません。**

元要求は「非harnessの他repositoryに本repository固有Planを必須にしない」です。Plan読取エラーを別の早期エラーへ変更しても、Planなしで通常Taskを開始できない点は解消していません。現差分の早期診断は先行整備として利用できますが、README-onlyの拒否を正しい最終behaviorとして固定しないでください。

既存ownerを確認したところ、appのactivationだけでなく、`Workflow.canonicalWorkflowAdmission()` が `controller.Activate()` を呼び、さらに `controller.Activate()` が `ResolveCommittedTaskAuthority()` を必須にしています。単にappでController activationをskipしても、mutating model callにcanonical authorityが必要な現行guardとは接続しません。

したがって、**現在のsourceに「Plan不要でそのまま使える正規entrypointが既にある」とは指示しません。** 採用する方向は、現在のworkflow/Controllerの単一実行ownerを維持し、Task authorityの確定とrepository-specificなPlan/Task harnessの適用を分離することです。旧worker復活、Controller/model mutation guardのskip、Plan自動生成、架空のTask path/Git identityは採用しません。request-backed authorityの具体的な永続形式や初期化順序を、その場しのぎで追加しないでください。

この不足接続は**Codexが引き取って仕上げる範囲**です。Web GPTは新しいschema・第二authority・互換経路を独断で増やして完了を目指す必要はありません。変更済み部分、まだ通らない正規入口、必要なauthority/guard接続を区別して引き継いでください。

### 判断2：markerなし・committed Plan/Taskあり

**「旧mainで動いていたから」という理由の `existingPlanBackedTaskAuthority` 特例は採用しません。** Planの存在だけでharness opt-inや実行authorityを推定する経路を、互換目的で維持しない方針です。

marker不在のrepositoryは、Plan名のfileがあるかどうかだけでharness activeへ昇格させず、非harness側の正規契約で扱います。現在の「通常入口だけPlanを根拠にController起動できる」という例外を最終契約にはしません。

明示 `--authority controller-activate` は現行のproject harness操作としてactive opt-inを要求する境界を維持します。通常のgeneric Task入口とこの明示操作の差は、利用するfeature/authorityの差として説明し、旧挙動を救う隠れたfallbackにはしません。generic側の正規接続と必要なCLI仕様の整合はCodex側で確認します。

### 判断3：非Git directory

**現在のTaskでは非Gitのmutating Task実行を正式に非対応とします。** Git common lineage・workspace/source snapshotに依存する現行の安全契約を、偽のGit identityや別stateで代替しません。

この点は、Plan読取等へ進む前に明示的なunsupported errorを返す現方針を採用します。非Gitまで対応する新しい実行基盤は本Taskへ追加しません。これは判断1の「通常GitでPlanを要求しない」という対応範囲とは別です。

### 継続採用する保護と残作業

malformed/untracked/nonregular marker、tracked markerのworktree削除・staged deletionをgenericへの降格に使わせない保護は採用します。active Task/leaseや既存のactivation記録がある場合も、現在のmarker不在だけで別authorityへ切り替えないでください。HEAD/indexの追跡確認と、必要な実行境界でのfreshness再検証を維持します。

開始前の判定とlock内の判定は異なるfreshness境界です。単なる重複として片方を削るのではなく、共通predicateと実行時再検証として整理してください。admissionを拒否した場合のController生成・runtime開始・model呼出しの有無も区別します。

[現headのpush CI #41525335685](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41525335685)は既に結果が出ており、Repository Lintはfailureです。

- `repositoryharness.go:103-104`：自然言語source commentによるcommentlint違反。
- `app/execution.go:277`：`activateWorkflowConfig` のcyclopが13、上限12。
- app/controller/test-2はcancelled、vet/buildとinstall-smokeはskipped。test-1のsuccessを全体PASSと扱わないこと。

Lintは意味のある責務分離と規則に沿う修正で解消し、guard緩和や抑制は行いません。scripted runnerによるactive側の正規dispatchは有効な接続検証ですが、README-only側が拒否されるtestでgeneric dispatch成立を主張しないでください。実モデルは未実行なので実モデル成功とも表現しません。

本PRは**部分採用・未完成の引継ぎ候補**です。Web GPT側では仕上げられる既知の修正と証拠整理まで進め、Codex引取りに必要な未達項目を残してください。Task/Planを先行削除せず、最終採否・Local Squash・必要な本配置/smoke・Metadata同期はCodexが担当します。checkpoint先頭の順序は変更しません。
````

none

### 2026-11-03 Codex巻き取り時の設計採否

- 非harnessの通常Git Taskは、受領したrequestを既存Controllerの正規authorityとして保持する。`SemanticTaskRef`はrepository内のTask file pathとrequest IDの排他的な二種を表し、requestへ架空のTask pathを割り当てない。request IDは実際に生成した一意なIDとする。
- request本文・ID・digest・実際のrepository HEADを既存ProjectSnapshotへ束縛する。第二のstate DB、Plan/Task fileの自動生成、旧worker経路、mutation guardの迂回は追加しない。Runtimeのrequestはこのauthorityと一致するものだけを実行する。
- generic起動も既存のbootstrap、attempt/lease、workspace snapshot、model-call admissionとmutation journal、Runtime bindingを使用する。Task fileが存在することを前提とする操作はauthority種別で分離し、requestの場合に本文・ID・digestの検証へ接続する。
- markerの現在値だけで既存authorityの種別を切り替えない。missing tracked marker、malformed/untracked/nonregular marker、実行中のharness境界変更はfail closedとする。markerなしのPlan名fileはharness opt-inの根拠にしない。
- request-backed Taskの正常dispatch、保存済みrequestとの不一致拒否、workspace/leaseの検証、再開、review/acceptに至る主要経路をscripted runnerで検証する。repository固有のPlan/Taskを要求せず、偽のGit identityや検証PASSは作らない。project scheduleを必要とする明示操作は、generic Taskへ架空のscheduleを作って対応しない。
- この採否に沿う具体実装はCodexまたは必要に応じたGLMが行える。schema・責務の追加変更が必要になった場合の採否はCodexが行う。今回の7件の巻き取りにparent-onlyは使用しない。

### 2026-11-03 request終了と次request開始の採否

- request-backed Taskは、既存のaccepted candidate・同一commitのpublication・completion readiness・Runtime acknowledgementを通して終了する。project Task fileの削除や次scheduleを捏造しない。requestの場合にterminal metadata fileが0件であることを型付きで検証する。
- 完了したrequest本文・実IDは既存ProjectSnapshotのcompleted request variantに保持し、選択中のRoot/Execution Taskは空にする。未完了のrequestを新requestで置き換えず、finalizedな終了証拠と完了Runtimeを確認した後だけ、新ID・新Runtime namespaceで通常Taskを開始する。
- bare Git remoteを使うscripted testで、通常Task入口からworker/reviewer、親accept、Local Squash、publication、完了ack、次request開始までをState補正なしで通す。これはgeneric利用のauthority分離の範囲であり、今回のイレギュラーな巻き取りを通すためのproduction変更にはしない。

- generic requestの終了に必要なcontroller-publication / controller-execution / evidence readは、既存のrequest authorityを持つrepositoryに限り正規入口を接続する。明示controller-activateとproject scheduleのsemantic操作はharness専用のままにし、markerだけを根拠とする種別切替や新規authorityの暗黙生成はしない。

### 2026-11-03 execution treeのscope確定

- execution treeのparent-managed metadata正規化は、markerを各低レベル関数で再推定せず、admissionが確定した`SemanticTaskRef`の種別へ束縛する。project Taskは既存のparent-owned metadata正規化を保持し、request TaskはPlan名fileを含めた通常sourceをそのまま保持する。
- tree capture / candidate normalization / suspension rebindには確定したTask種別を渡す。markerのfreshnessとauthority種別切替の拒否は正規admissionで維持し、既存projectのsource処理をgeneric扱いへ降格させない。旧signatureのaliasや推定fallbackは残さない。

### 2026-11-03 実装担当のユーザー確認

原文:

````text
実装はCodexがやってるよな？GLMにやらせてないよな？
````

- 巻き取り実装・追加修正・設計判断・最終採否はCodex自身が行う。途中のGLM調査callによるコード変更はなく、Codexが直接作業を継続する。

## Resolved references

- `Review.md` の「2026-10 再監査」の A8。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

通常Task入口がharness inactiveの他repositoryにもcommitted Planを必須にする不整合を修正する。repository固有guardとgeneric executionの境界を親判断で確定する。

## External feasibility

status: not-applicable

## Contract

- marker/Planのない他repositoryへ本repository固有guardを一般化しない規則と、activateWorkflowConfigの無条件controller activationを整合させる。
- 非harness利用で必要な現行task authorityと実行入口を親が確定し、明示controller-activateとのadmission差を説明可能にする。repositoryharnessの既存判定を再利用する。
- harness active側ではcommitted Task/lease/parent metadata不変性の検証を維持する。非Git利用も正規support範囲を明示し、未対応を曖昧なPlan読取失敗へしない。

## Must not

- 旧worker経路、dual authority、Plan自動生成、旧schemaやaliasへの互換fallbackを復活させない。
- markerを消してguard回避する実装やactive側のfail-openを追加しない。
- 今回のfixtureはmodel未呼出しのadmission再現であり、実モデルの成功を主張しない。

## Acceptance criteria

- READMEだけをcommitしmarker/Planがないrepositoryで、通常Task入口が本repository固有Planを暗黙要求しない。
- active/inactive/malformed marker、明示controller操作、正規非Git利用方針の各入口を検証する。
- active task/lease/metadataの既存保護を維持し、正規dispatchまでのscripted runner testとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
