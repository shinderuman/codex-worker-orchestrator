# 親model-visible evidence境界

親Codexがglm-worker管理下のevidenceをSol Highへ投影する境界。目的は判断に必要な証拠だけを一度受け取り、既知・不変本文の再投影と細粒度pollによる消費を減らすことである。

## 利用方針

- 復帰後の再anchor、terminal packet後の補完、複数surfaceにまたがる判断では、細粒度readをturn列へ展開する前に`glm-parent-action evidence <manifest.json>`へ必要evidenceをまとめる。
- 何を判断材料とするか、semantic question、対象surface・path・regionは親が決める。追加本文が必要な場合だけexact source/diff regionを要求し、同file全体や広範diffを先読みしない。
- projection/dedupは`control:parent-evidence-projection-dedup`、manifest/result/read-scope/budget/refinementはcurrent parent-evidence runtimeが機械強制する。親はそのschema・lease・duplicate判定・exact argv/orderを自由言語で再構築せず、machineがrefinementを要求した場合はsemantic questionやlocatorを絞る。
- authorityやrepo-search固有の意味契約は各canonical ownerを正とし、このgeneric evidence境界で再定義しない。

## 強制できない残余境界

- 親は、どのevidenceが意味的に必要かと、投影されたevidenceをどう解釈するかを判断する。
- 任意shellでの`rg`・広範`git diff`・JSON全文読みはrepository runtimeで阻止できず、強制可能と偽らない。それらを選んだ場合もowner callで既に得た読みを重複させない。
- 消費評価が必要な場合はownerが返すsummary / telemetryを使い、評価のためにraw evidence全体を再投影しない。
