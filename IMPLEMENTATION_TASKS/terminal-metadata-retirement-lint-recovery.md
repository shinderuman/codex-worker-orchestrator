# Task: Terminal Metadata退役生成のlint整合と正規復旧案内

## Original instruction

2026-11-02 Dogfood Audit（後続Bundle `a96c5720-c8e9-4079-91dd-8372f956359d(1).zip`）で確認した、正規PublicationのMetadata生成失敗と実行できない復旧案内を修正する。実装を変えずState surgeryで回避できたことを、正常な製品動作とは扱わない。

## Amendments

none

## Resolved references

- 現行Task `worker-docker-test-owner-isolation.md`、実行元 `23744ba0f6f370fa80b49de3c39ff222508e1ac3`、最終公開 `ecf8af99c85e6a8dddae19bdc6043cef84b2dc3a`。同一Taskの中断Bundleを継続監査するAudit #1461（管理番号は実装正本ではない）。
- 親生ログ `bound/transcripts/parent/01a19716-0a87-7453-93fd-3270be90d33d/0`。2026-11-01T18:51:33Zに通常 `controller-publication` 開始、正規race gate・install完了後19:06:52Zに `completion final Metadata lint failed` で公開前停止。Plan退役でNEXTからsuccessorを取り除いた空行が連続したことが違反の根拠。source/final/remoteは `3fc89e17.../23744ba0.../23744ba0...`、remote未変更。
- `glm-worker/internal/taskcontract/retirement.go:RetirePlanTask` は `retirementPlanLine` が削除対象のNEXT行へ空文字列を返し、元の行を空行に置換する。元からある空行を正規化せず `strings.Join` するので、`harnesslint.RunCommit` が退役後の新Planで空行規則を拒否する条件を作る。
- `glm-worker/internal/controller/publication_completion_readiness.go:completionMetadataLint` は正当にfail-closed。検証を緩めるべきではない。
- `glm-worker/internal/controller/publication_abort.go:finishPublicationAbort` は一定条件で `PublicationAdvancementRequired{Action:"reenter-accepted-candidate"}` を返すが、当該生ログでは親が `glm-parent-action prepare controller-publication --operation reenter` を実行し `completion operation reenter is not admitted at publish` と拒否された。失敗後のtyped handoff・正規admission・命名対応を調べ、現実に合法な次操作を提示させる必要がある。エラー文字列だけで操作を捏造しない。
- 親は19:07–19:29にControllerとStateを広く調べ、ユーザーのState変更許可に基づく復旧を実施。その後 `runtime evidence belongs to another attempt` を検出して `controller-runtime-binding.json.attempt_id` を手動修正した。最終的にrace/Install/Publishを再実行して19:58Zに公開成功。手動binding書換えの成功を正規復旧契約としない。
- 成功後に発生した `parent_action_terminal_handoff_failed` は既存の `post-publication-terminal-handoff-scope-failure.md` が所有する別境界。GLM Review特例とCodex実行許可reviewerの拒否も本Taskのownerではない。

## Purpose

正規Completionが生成するPlanを自身のMetadata lintで拒否しないようにし、実際に公開前のMetadata不整合が起きた場合にも、親へ実行可能な正規復旧操作・拒否理由を提示する。親Codexによる20分規模のコード探索やController内部State書換えを不要にする。

## External feasibility

status: not-applicable

## Contract

- `RetirePlanTask` と `repositoryproject.RetireTerminalMetadata` が生成するPlanを、元のACTIVE/NEXT/BLOCKEDの並び・Markdown link形式・Task同一性・依存関係を保って正規化する。削除したNEXT行を空行として放置せず、生成物を同じ最終lintで検証する。
- `completionMetadataLint` による本当のMetadata違反の拒否は維持する。正常に生成した退役Planがフォーマット規則だけで不正扱いされないよう、producer側を修正する。
- 公開前のlint拒否→abort→candidate reentry / preparation→最終Publicationへ至る既存ownerの対応を検査し、実行できない名前・stageの操作を復旧指示として返さない。正規のtyped commandがあるならそのまま示し、ないなら必要な状態と理由をfail-closedで返す。terminalなsource/remoteの真正性とgeneration、lease、候補を保持する。
- 同じ正規fixtureでの復旧を通し、ユーザー手動の`controller-runtime-binding.json`や`parent-review-state.json`の直接編集、証拠再利用イベント捏造、モデル再実装や同一sourceへの不要なfresh Gate再投入を前提にしない。
- 内部State不整合を隠すために復旧時のruntime/attempt identity Guardを緩めない。user許可に基づく今回の一回限りのState surgeryを恒久的な自動修復へ一般化しない。

## Must not

- 新しい第二State、汎用repair daemon、旧世代互換fallback、Codex直接実装経路、過剰な自動再試行・新モデル呼出を追加しない。
- Metadata lintや候補・remoteの一致検査を無効化しない。実際にsourceが異なる場合はfail-closedを維持する。
- 別ownerのpost-publication terminal handoff failure、GLM Reviewのユーザー特例、外部permission reviewerの承認問題を同Taskへ混入しない。

## Acceptance criteria

- 正規ACTIVEと複数NEXT、元々の見出し間空行を持つ実用的Planで、Terminal Metadata生成→commit→`harnesslint.RunCommit` がPASSし、後続ACTIVEやNEXT順序・リンク形式・依存状態が正しい。NEXTが複数／1件／空の境界を確認する。
- 出版直前に合法なMetadata不整合を意図的に注入したE2Eでは、fail-closed、remote未更新、候補保存、型付き復旧指示の実行可能性が成立する。拒否後に親が案内された操作を実際に実行し、State直書きなしで正規候補/公開へ収束する。
- 本物の不正候補、stale generation、別Attempt/runtime evidence、遠隔進行、再入場できない状態は正しく拒否する。
- 実親handoff・staged CLIの経路を検証し、Controller単体の合成PASSを正規復旧の証明としない。修正前後の親tool/model往復・validation再投入・wall-clockを可能な範囲で記録する。
- 関連Go test・lint、独立Reviewを実施する。後続Dogfood未観測なら実効性は未実証と明記する。

## Historical invariants

- Current Git / Rules / Plan / 本Taskを実装正本とする。1 Task 1 Commit、正規GLM Worker、Codex semantic判断、独立Review、immutable evidenceと権限境界を維持する。
- 既存ownerが完全に同じ不変条件を解決したと証明できた場合は重複実装しない。単にエラー表示が消えたことを修復完了としない。

## Dependencies

none
