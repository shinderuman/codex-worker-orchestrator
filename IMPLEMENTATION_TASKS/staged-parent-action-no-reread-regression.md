# Task: staged parent action no reread regression

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- closed #319はstaged decision/fix transportを`prepare -> validate returned JSON/path/token -> exact apply_patch -> staged action`の同一tool orchestrationへ縮約し、freshly prepared `.glm-worker-parent-actions/*` placeholderのstandalone rereadを0にするlive Acceptanceでcompletedした
- formal Dogfoodではfresh prepare後のstaging fileへ`sed -n`を行うstandalone rereadが5回再発した（decision 1回、fix 4回）
- current instructionsもprepare直後のstaging file rereadを禁止しているため、既知templateを確認するためのrereadはsemantic evidenceではなくtransport-only parent re-entryである
- #866後にdecision templateが拡張されたが、templateはmachine-owned prepare contractでありrereadを正当化しない
- `parent-fix-origin-cause-staged-transport.md`はorigin/cause metadata欠損を所有し、本taskのno-reread regressionとは別rootである

## Purpose

staged parent actionでprepare済みplaceholderをparentが再読せず、既知prepare contractと返却path/tokenから同一tool orchestration内でexact edit/actionへ進む#319 contractを後続action/template変更にも耐える形で再固定する。

## External feasibility

status: not-applicable

## Contract

- `prepare decision|fix|start-milestones|revise-milestones`等、staging placeholderを使うcurrent action familyで、prepare成功後のnormal pathはreturned JSON/action/token/pathをvalidateし、その既知placeholderをexact editしてactionをinvokeするまでparent modelへ戻らない
- freshly prepared staging fileのcontent確認を`cat`/`sed`/read tool等で行わない。expected placeholder/header/template shapeはproduction prepare contractを正とする
- action/template fieldが増減した場合も、prepare contract/schemaとstaging writer側を同時に更新し、parent rereadをcompatibility fallbackにしない
- malformed prepare output、path/token mismatch、placeholder replacement failureはaction invocation前にfail closedする
- one-shot consume、path/symlink/size/token-binding、standard edit boundary等のexisting staging safetyを維持する
- semantic payload作成はparent Codexが所有し、transport縮約のためにGLMへ移さない

## Must not

- reread禁止をproseだけ追加して完了しない。後続action/template変更でregressionを検出できるmachine regression/evalを持つ
- generic staging file readをすべて禁止してdebug/recovery evidenceを失わない。対象はfresh prepare直後のnormal transport choreography
- shell redirect/heredoc/arbitrary writerでstandard staging edit boundaryを迂回しない
- semantic decision/fix contentをmachine生成してmodel callを削減したことにしない

## Acceptance criteria

- representative decision/fix staging pathで`prepare -> validate -> exact edit -> action`が一つのtool orchestrationに収まり、fresh staging file standalone readが0である
- current milestone staging actionを含むtemplate/action family変更fixtureでもno-reread invariantが維持される
- malformed prepare/path/token/placeholder fixtureはaction実行前にfail closedする
- staging safety（one-shot consume、symlink/size/token binding、standard edit boundary）が不変である
- parent behavior/evalまたは同等のregressionが、formal Dogfoodで再発した`sed -n .glm-worker-parent-actions/...` choreographyを拒否する
-追加model callなし、Repository Lint、関連Go test/full suiteがPASSする

## Historical invariants

- prepare直後のknown placeholder rereadはsemantic judgmentではなくtransport-only churnである
- semantic payloadはparent-owned、staging transportはmachine-ownedである

## Dependencies

none
