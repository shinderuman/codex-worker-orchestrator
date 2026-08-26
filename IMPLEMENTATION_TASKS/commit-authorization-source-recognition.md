# Task: commit authorization sourceのfalse negative停止を防ぐ

## Original instruction

`````text
いやそれを停止させてるのはどのルールなの？このリポジトリ内のルール？
`````

`````text
解釈が間違ってるだろ
いままでずっと止まらずにやってきたじゃねえか
停止したことがバグだよ
それ自体もタスクにあげろ
`````

## Amendments

### Amendment 1

`````text
いやだからなんでPushしないんだよ
ルール変えてないの？
`````

### Amendment 2

`````text
やれ
`````

## Resolved references

- 「それを停止させてる」は、Task 009がimplementation・verification・review・親採用まで完了した後、親Codexがtask completion commitを2回試み、実行承認境界が「ユーザーによる明示的なcommit依頼が確認できない」と拒否して停止した事象を指す。
- 拒否前のACTIVE task requirementには、ユーザーが添付したlossless指示として「implementation / required verification / independent review / 必要なSol/Codex品質gate / task completion metadata同期 / commitまで正常完了すること」と明記されていた。
- 親Codexは拒否後、添付内commit指示と継続要求を根拠に再試行したが、同じ理由で再拒否された。
- 「解釈が間違ってる」は、明示許可の有無を最新の短い会話メッセージだけで判定し、ACTIVE taskのOriginal instruction・Amendments・添付lossless sourceをauthorization sourceとして扱わなかった解釈を指す。
- 「いままでずっと止まらずにやってきた」は、既存task lifecycleではtask requirementにcommitまで含まれる通常作業を局所gate後に親Codexが継続してきた運用実績を指す。
- Amendment 1は、Greptile日次reviewのauthorityをremote mainとremote checkpoint refへ移した後も、旧AGENTS/git instructionの一律push禁止を優先して親commitをremote mainへ反映しなかった事象を指す。
- Amendment 2は、本repositoryで親Codexがfinal commit後にremote `refs/heads/main`を通常fast-forwardする既存RULESを直ちに実行し、同じfalse negativeを止める明示指示である。

## External feasibility

status: not-applicable

## Purpose

明示的なcommit許可がACTIVE taskのlossless requirement sourceに存在するのに、最新メッセージ単体だけを見て「許可なし」と誤判定し、正常なorchestrationを停止するfalse negativeを再発不能にする。

## Contract

- commit authorizationは文の配置場所ではなく、現在taskへ適用される明示的なユーザー意思の有無で判定する。
- ACTIVE taskのOriginal instruction・Amendments・Resolved references、ユーザー添付のlossless source、同一taskへ適用される会話上の明示指示をauthorization sourceとして扱う。
- task requirementが明示的にcommitまで要求し、scope・対象repository・task境界が一意なら、最新メッセージでcommit語を再度要求させず既存lifecycleを継続する。
- 実行承認境界がrepo instructionの意味より狭く解釈する原因を一次証拠で特定し、repository側で強制可能ならcanonical instruction・wiring/testを最小修正する。
- repository外のapproval layerが原因でrepoから修正不能なら、repo内で解消済みとせず外部境界を明示し、同じfalse negativeを通常完了扱いしない。
- 本repositoryではGreptile運用のため、各final parent commit後のremote `refs/heads/main`通常fast-forwardを既存の継続許可として認識し、commit単位で再許可を要求しない。
- GLM commit/push禁止、対象task外のcommit禁止、force/non-fast-forward、tag、他ref、他repositoryへのremote書込み禁止は維持する。

## Must not

- 「過去にcommitしてきた」ことだけを包括的な将来commit許可へ拡張しない。
- commit語を含まない一般的な継続指示だけを無条件commit許可として扱わない。
- 本repositoryのremote main fast-forward許可を、force操作、tag、他ref、他repositoryへ拡張しない。
- approval bypass、別command、shell wrapper等で外部拒否を迂回しない。
- worker/reviewer promptへ同じauthorization説明を重複追加しない。
- 実行環境外部のfalse negativeをrepository codeの修正だけで解消済みと報告しない。

## Acceptance criteria

- Task 009 commit拒否2回の入力指示・実行要求・拒否理由を一次証拠として再現可能に整理する。
- `git commitは明示依頼時だけ`という安全規則と、ACTIVE task lossless source内の明示依頼をauthorizationとして認識する規則を矛盾なく統合する。
- 最新メッセージ単体にcommit語がなくても、ACTIVE task Original instructionに対象taskのcommit要件が明示されていれば継続可能なcontractを固定する。
- commit許可がどのsourceにもないnegative caseは従来どおり拒否する。
- task scope不一致、別repository、別task、GLM push、force/non-fast-forward、tag、許可外refを許可しないnegative境界を維持する。
- `IMPLEMENTATION_RULES.md`、repository/installed `AGENTS.md`、Git操作instruction、Planでcommit/push authorization sourceと許可refの受理集合を一致させる。
- repository内で強制可能な境界には最小のscenario/wiring verificationを追加し、instruction文面だけを再発防止証拠にしない。
- repository外の承認境界が残る場合はBLOCKED等の適切な外部境界へ残し、原因・影響・手動fallbackを明記する。
- 親Codexが最終reviewし、通常task lifecycleに従ってcommit後、remote mainへ通常fast-forwardする。

## Historical invariants

- GLMはcommit/pushしない。
- 親Codexはtask/review-follow-upの必要gate通過後、当該taskに明示されたcommit要件に従ってcommitできる。
- remote書込みは原則としてlocal commit許可から推移しない。ただし本repositoryではGreptile運用の恒久RULESにより、親Codexのfinal commit後のremote main通常fast-forwardだけが明示済みの継続許可である。

## Dependencies

none

## Review findings

none

## Current boundary

Task 009完了時のcommit承認false negativeを3回観測し、Greptile運用開始後にはremote main fast-forwardの恒久許可を旧一律禁止で上書きするfalse negativeも観測した。ユーザーの直接再指示後は親commitとremote main pushを実行済み。install smoke loop cost削減を先行し、その完了後にcanonical instructionと実行承認境界を同期する。
