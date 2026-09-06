# 親model-visible evidence境界

親Codexがglm-worker管理下のevidenceへ接する機械境界の運用契約。目的はSol High判断に必要な証拠だけを一度受け取り、既知・不変本文の再投影と細粒度pollによる消費を機械的に削減することである。

## 一括owner call (`glm-parent-action evidence`)

- 復帰後の再anchor、terminal packet後の補完、複数surfaceにまたがる判断材料の収集では、`--handoff`・`--status`・`--repo-search`・raw `rg`/`git diff`を個別に並べる前に、`glm-parent-action evidence <manifest.json>`で必要partを1回のowner callへ集約する。
- manifestは`version:1`と`reason`必須で、少なくとも1つのpartを指定する。partはauthority(hash差分)・handoff(known_digest)・status・validations・telemetry・search(question+scope+budget)・diff(question+paths+budget)・source(question+path+行範囲+budget)から選べる。
- 出力はschema付きbounded JSONで、各partはstatus・digest・bytes/token proxy・exact locatorだけを返す。既読hash一致のauthorityと既読digest一致のhandoffは`unchanged`となり本文0 byteである。budget超過partは`refinement_required`と理由を返し、raw切断本文は出ない。欠損fieldは`unknown`/`error`のまま扱い、whole-document fallbackを行わない。
- 追加本文が必要な場合はmanifestのsource/diff partでsemantic questionとexact locator(path・行範囲)を指定して取得する。必要なければ同file全体や広範diffを先読みしない。

## 重複投影の機械拒否

- 同一decision lease内(taskがwaiting-sol-review/waiting-decision/parkedの間)で、既に投影済みdigestと同じ`--handoff`・`--handoff recovery`・`--status`・`--repo-search`を単独再実行すると、structured error `duplicate_parent_projection`(surface・digest・owner_call_id・batch_command付き)で拒否される。
- これは失敗ではなく重複投影の機械拒否である。拒否されたら同じ読みを繰り返さず、`detail.batch_command`のevidence batchへ読みを集約する。親actionによる状態変化後はdigestが変わるため通常どおり受理される。
- 親model return回数を削減するため、複数surfaceの読みが必要な場面では最初からevidence batchを使い、細粒度読みをturn列へ展開しない。

## authority再読とrepo検索

- authority再読境界では`glm-worker --authority <kind> --known-content-sha256 <hex>`を使う。同一hashなら本文0 byteの`unchanged`、変更時だけ`changed`と本文が返る。3kind出力の`authority_snapshot_sha256`一致確認と`active_task`一致確認は従版どおり行う。
- `glm-worker --repo-search`は`<question> --scope <path|symbol:identifier> [--scope ...] --budget <bytes>`が必須形式である。候補・budget超過時は`refinement_required`と理由が返るため、scope追加・絞り込みかbudget引き上げで再依頼する。

## 強制できない残余境界

- 任意shellでの`rg`・広範`git diff`・JSON全文読みはrepository runtimeで阻止できず、強制可能と偽らない。それらを選んだ場合でもevidence batchで表現可能な読みを重複させない。
- 検出telemetry(stateの`parent-evidence.jsonl`)にsurface別bytes/token proxy・parent return回数・重複digest・refinement回数が計上される。Direct Codex対Codex + glm-worker評価の消費判断にはこのtelemetryと`evidence`出力の`evidence_summary`を使う。
