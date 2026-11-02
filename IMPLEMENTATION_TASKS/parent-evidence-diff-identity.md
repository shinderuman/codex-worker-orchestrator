# Task: 親向け差分証拠の同一性とdedup

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
親へ届ける差分が変わったときに、同じdigestを理由に既読として省略しない。mode変更とdirectory配下の変更を含む正規path契約を確定する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1486#issuecomment-6588457120

````text
実装前のCodex判断として、**A：directory / Git pathspecを非対応とし、repository相対のexact file pathだけを受理する**方針を採用します。本Taskでdirectory/glob展開の新しいownerは追加しません。

確認した基点は `e3dcec1525adbee32e33078928510c94dcdbb404`、PR headは `e70e733d0e3136aa794b29fe044531b2aae9991b` です。TaskのContractとA3の元要求、現行のcapture/delivery/proof経路、本PR差分を確認しました。以下は実装方針の採否であり、現在のPRの完成認定・CI PASS・Merge承認ではありません。

### 正規入力の境界

- `Paths` はrepository相対のexact file pathの列挙とします。空path、repository root、`.`、親directoryへの遡行、絶対path、NUL、末尾スラッシュ、directoryを明示拒否してください。削除済みdirectoryも、HEAD/indexがtreeであることを根拠にfileとして受理してはいけません。
- glob（`*` / `?` / bracket pattern）とGit magic pathspecは不対応として明示拒否します。これらの記法を含むfilenameも本入口では非対応とし、暗黙展開や旧pathspec互換fallbackを作りません。空白・引用符・先頭ハイフン等の、展開記法ではないfile名はshell解釈せずliteralに扱ってください。関連するdiff/status/ls-filesも同じliteral path契約へ揃えます。
- **削除済みtracked fileは受理します。** worktreeに存在しなくても、HEADまたはindexにそのexact pathのfile entryがあることを確認し、worktree不在を削除状態としてidentityへ含めます。HEAD/index/worktreeのいずれにもないpathは、typo等を空の成功証拠にしないため拒否します。
- **untrackedのregular fileとleaf symlinkは受理します。** status/type/mode/content identityを取得し、内容変更でdigestが変わること。untrackedをGit diff本文の対象外だからとidentityから落としてはいけません。本文を届けていない状態を、hashだけで本文review済みのproofに昇格させないこと。
- **leaf symlinkはリンク自体を対象とします。** `Lstat` と `Readlink` によりliteral targetを扱い、参照先の内容は読みません。dangling linkやrepository外を指すleaf linkも、リンク自体の証拠としては受理できます。中間componentのsymlinkは本入口では内外を問わず拒否し、path aliasや外部参照として辿りません。
- FIFO/socket/device、directory、gitlink等の非対応typeと、indexのunmerged複数stageなどidentityを一意に取得できない状態は明示拒否します。permission/I/O失敗も空hashへ縮退させません。

### 既存差分の採否と必要な修正

実際のraw patchと `DiffBody` のmetadataをdigestへ結び付け、regular file / symlinkのtype・mode・内容identityを含める方向は**継続採用**します。既存のdelivery ledger・dedup・review-proof ownerを利用する方針も維持してください。旧digestのalias/migrationや常時全量再送は不要です。

一方、現headの次は修正が必要です。

1. directory成功系fixtureは、directory入力の明示拒否へ変更する。対象fileを個別指定した正の回帰は残す。
2. `diffWorktreeIdentity` のdefaultにある「空hashとmodeを返して成功」は、非対応typeのエラーへ変更する。
3. `IsNotExist` を無条件に成功扱いせず、上記HEAD/indexのexact file entry確認と組み合わせる。現在のrenameテストは変更前に未存在の `moved.go` も要求しているため、この契約に沿ってfixtureを直す。
4. repository境界/component検証を、対象を辿るfilesystem readより前に行う。leafをLstatした後のtype差替え等も考慮し、既存のread-scope/snapshot guardを維持する。途中の入替えでrepository外の内容を読む経路を残さない。
5. HEAD/indexの照会失敗をすべて「entryなし」として握り潰さない。正当な未追跡/削除と、Git実行失敗・不正/曖昧なentryを区別する。

digestの採取時点と実際の配信対象を揃え、採取途中にsourceが変化した場合は再取得要求またはエラーにしてください。異なる時点のpatchとfile metadataを組み合わせた証拠を配信しないこと。

### 完了確認

- directory（既存/削除済み）、glob、magic、全く存在しないpath、非通常file、中間symlinkの拒否を正規manifest→projection→Commit経路で確認する。
- exact fileでcontent、mode-only、index-only、regular↔symlink、link target、untracked内容、rename/deleteを検証する。変更は再配信され、同一再要求はdedupされること。
- budget不足の `refinement_required` や最終output budgetで本文を落とした場合、未配信の本文がdelivery済み/proof済みにならないこと。scope/lease変更、出力失敗、source変更後の古いproof拒否も維持する。
- 最終exact SHAで関連test、repository lint、必要な正規gateと独立Reviewを通す。現在の部分的なCI結果を最終実装のPASSへ流用しない。

この方針で本PRの実装を完成させてください。Task/Planは親Codex専有なのでWeb GPT側で先行削除せず、更新候補と根拠を返してください。全Acceptanceの採否とMetadata同期はCodexが行い、checkpoint先頭の順序は変更しません。
````

## Resolved references

- `Review.md` の「2026-10 再監査」の A3。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

親へ届ける差分が変わったときに、同じdigestを理由に既読として省略しない。mode変更とdirectory配下の変更を含む正規path契約を確定する。

## External feasibility

status: not-applicable

## Contract

- parentevidenceのdiff identityを、実際に届ける対象のcontent/type/modeと差分の意味に一致させる。questionや先頭index blobだけで同一と判断しない。
- directory/pathspecを正規入力とするか、明示拒否するかを実装前に親が確定する。許可する場合は全対象をidentityへ含め、regular-file hashを取れない対象を空identityの成功にしない。
- 同一証拠の再送削減は維持し、変更された証拠だけが再送されるようにする。deliveryとreview proofのbindingも確認する。

## Must not

- 旧digestのalias、migration、旧path仕様への互換fallbackを追加しない。
- 常時全量再送やdedup全廃を無検証の解決策にしない。
- 本文だけ更新してdelivery ledgerのidentityを旧方式のまま残さない。

## Acceptance criteria

- mode-only変更と、directory指定後の子file変更で、意味の違う差分がunchanged扱いされない。directoryを非対応にする場合は明示拒否を検証する。
- content/rename/delete/untracked/symlinkを正規path契約に沿って検証する。未変更の再要求は正しくdedupする。
- 表示内容とdigest・delivery/review proofが対応し、変更検出のregression testとquality gateが通る。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
