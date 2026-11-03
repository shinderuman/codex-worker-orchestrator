# Task: 通常FacadeのGuardian審査適格性と実配備policyの一致

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

閉鎖済み#320以降も普通のglm-parent-action facadeが審査された複数の現物（#1341 C07、#1346 F07、#1347 J06、#1356 F05）。一方、複合shell/直接書込/本install/remote Pushは権限審査の必要性が異なる。
- 2026-10-29 Dogfood Audit [#1423](https://github.com/shinderuman/codex-worker-orchestrator/issues/1423) / raw Bundle `12ae3721-8dbe-4ac9-8d40-a8490d28f17c.zip`: Guardian raw transcript `bound/transcripts/guardian/01a183ae-54b1-74d3-8570-a935d3b0a851` に37回の自動審査（allow 36 / deny 1）。約89.239秒の審査処理時間合計・3,279,786 model total tokens（cache input 2,986,496を含む）。これらの呼出の大部分はユーザー承認に基づくCodex直接実装の作業であり、GLM通常Worker全体のGuardian負荷へ機械的に一般化しない。
- 同Bundleの2026-10-28T18:35:44Zで正規`controller-publication`が`origin/main`の具体的なPush許可なしと判定されDeny、親Codexが許可を質問、18:37:09Zにユーザーが許可、18:37:18Zに同じ正規完了操作がAllowとなった。Deny→ユーザー回答まで約85秒。**人間入力待ちが一度でも発生した時点で無人Workflow継続性は未達**。元Taskの完了指示にはorigin/mainを文字列で明記していないため、外部審査が権限契約上誤判定だったことまでは未証明。
- 既存Audit [#1399](https://github.com/shinderuman/codex-worker-orchestrator/issues/1399)の約53分の承認待ち、[#1420](https://github.com/shinderuman/codex-worker-orchestrator/issues/1420)のPublication審査2回拒否も比較対象。単なる件数でなく、ユーザー呼出を生んだ正規作業と、実際に保護すべき不可逆操作を区別する。


## Amendments

### 2026-10-29 自動レビューの実効性・無人実行阻害（ユーザー追加指示）

````text
正直自動レビューが助けになるより妨げになっている感じがある
妨げしか気づかないからかもしれないが
自動レビューがブロックする→Codexがブロック対象外にしていいかと聞いてくる→Codexが正しいので承認する
いまCodexで作業しているやつでも同様の妨げが出ている
次回のAuditが完了したあとMarkdownの本文を更新するPRを出せ
順序は改めてCodexに判断させるからそのままでいい
````

## Purpose

installed managed policy・実CLIバージョン・生argvを照合し、裸の正規Facade操作が意図通り扱われるか調べる。加えて、正規操作の過剰な自動レビューがCodex消費と無人Taskの継続性に与える実害と、保護の実効性を一次証拠で評価する。

## External feasibility

status: not-applicable

## Contract

installed managed policy・実CLIバージョン・生argvを照合し、裸の正規Facade操作が意図通り扱われるか調べる。不要なReviewだけを機械的に排除し、重要副作用は従来通りGuardに渡す。

- 実rolloutから生のGuardian審査ごとに、元のparent planned action（shell wrapper/裸のfacade/実ファイル編集/検証/本install/remote Push）、user authorization、`risk_level`、allow/deny、実際の副作用・安全性、再試行・許可質問・人間応答の有無を復元する。Guardian実行件数、Allowで完了した無意味な再審査、実際に危険を防止したDeny、同じ操作を人間が許可して続行したDenyを別々に数える。
- **正規Workflowの無人継続性**を明示的なQuality指標とする。ユーザー追加承認を待って止まった事象は1件でも「その実行の無人継続性未達」と記録する。Task成功・Guardian後のallow・レビュー件数が少数という理由で問題を消さない。同時に、真に追加権限が必要な特権副作用と過剰審査による誤拒否を混同せず、明示権限不足を勝手に解消扱いにしない。
- Guardian審査の実行時間、拒否→人間応答までの停止時間、追加parent model/tool往復、token input/cache/outputをsource-boundで分離して測る。安全上の便益は拒否の危険性・必要性を具体的に判定し、利益が実証できない場合はunknownを残す。cached tokensを独立した追加inputとして二重加算しない。
- installed policyとCodex app実権限境界を照合し、**リポジトリ側で直せる不要なFacade審査・コマンド構成**と、外部の自動承認判定・OS/sandboxの真の権限要求とを責務分離する。前者は根拠に基づき機械的に削減し、後者はリポジトリ変更では保証できない範囲を明示する。Codexがユーザーへ自動で解除許可を尋ね続けることを改善結果としない。

## Must not

全Guardian allow/denyを不要として数えない。特権操作のGuardを一括スキップしない。deny後に別操作が成功しただけで同一拒否のbypassと断定しない。

- ユーザーがTask完了を指示した事実と、`origin/main`等の具体的公開先についての権限が実際に付与されているかを同一視しない。Guardを全無効化したり、remote Push/forceなどの不可逆権限を暗黙許可に変更したりしない。
- `allow`率の高さだけでGuardian全体を無価値と断定せず、逆にReviewが存在する事実だけで安全性が改善したと認定しない。ユーザーが最終的に許可したことを、Denyが実は危険ではなかったとする単独証拠にしない。
- policyで禁止された操作を別のshell/Python/Perl wrapperへ置換して審査を回避する対策を導入しない。自然言語の注意書きだけで誤拒否が解消したと判断しない。

## Acceptance criteria

裸のfacade/複合shell/実install/publicationの認可結果が現在配備とsourceで一致する。必要なら変更前後実rolloutで呼出回数・Latencyと安全境界を比較する。現状が有効ならコード変更なしで処置できる。

- 複数の実Bundle（最低限#1423、#1420、#1399）について、Guardian invocation→判定→親の対応→ユーザー介入→元の正規作業への復帰まで具体的に結び付け、block/allowの正当性・不可逆影響・machineとexternalのowner・根拠の強さ（confirmed/unknown）を一覧化する。
- 変更が必要な場合、代表的な裸Facade、複合shell、ローカル検証、正規Publicationについて、変更前後の実rolloutでGuard呼出/不要deny、人間待ち発生件数と経過時間、Codex tokenとmodel/tool再入、Quality Deltaを比較し、**正当に許可された通常作業でユーザー介入がなく継続すること**を実証する。まだ観測できない場合はeffectiveではなくunprovenとする。
- 実際の危険な操作を防止したDenyと必要な追加承認は維持する。不要な審査を削減しても危険操作に対する本来のapproval/sandbox保護が失われないことを検証する。
- 原因が外部の自動審査policy等にありrepository変更で是正できない場合は、作用境界・再現根拠・現実的な採否と残存する人間依存を示し、現状を「修正完了」と報告しない。Codexが次のTask優先度を判断するため、Plan順序はこの変更で固定しない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
