# Task: Repo-search System-One relevance filter evaluation

## Original instruction

````text
https://zenn.dev/mazrean/articles/bd9b563ace18db

これに俺のCodex Orchestratorに活用できる要素はあるか
````

````text
これと言ったのはこの記事とこの記事から飛べるリンク先も含めた内容でだ
````

````text
IMPLEMENTATION_PLANとかもちゃんと見ろよ
````

````text
それは独立タスクとして入れるべきなの
````

````text
PRでIMPLEMENTATION_PLANに作るようにして
独立タスクは独立タスクとして反映して、そうじゃないやつもそうじゃないやつで反映して
````

## Amendments

none

## Resolved references

- 起点記事: https://zenn.dev/mazrean/articles/bd9b563ace18db
- 関連するSystem-One / Jev設計説明: https://typesafe.ai/blog/introducing-system-one-models-and-jev と https://evals.typesafe.ai/
- 関連するRAG評価: https://qiita.com/kikuziro/items/2be9091b328d8b844640
- 関連するJev reranker評価: https://secon.dev/entry/2026/09/20/100000-jev-reranker/
- 外部事例では、lexical/vector candidate取得後にJevで「queryへ答えるためのusable evidenceか」を狭い独立判定として評価し、低関連候補を後段から除外する構成が示されている。別の関連評価では、単なるrerankよりanswerability/no-answerの早期gateで後段LLM call自体を止める方が大きな削減効果を持つ例が報告されている。ただし外部corpus / benchmarkの精度・削減率は本repositoryへ外挿しない。
- current `IMPLEMENTATION_TASKS/repo-search-result-context-budget.md` と `IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md` はnormal pathへの追加model call / embedding / external retrieval依存を明示的に禁止するため、System-One relevance判定は両Taskへ混ぜず独立評価境界とする。
- current repo-searchはnavigation locatorでありsource proofではない。System-One filterを評価しても、semantic conclusionに必要なexact source inspection、reviewer independence、quality-required exhaustive proofのauthorityは変更しない。

## Purpose

current repo-searchが取得したcandidate群に対して、低costのSystem-One relevance / evidence-usability判定をshadowで挟むことで、後段のsource read、追加search、parent/model re-entry、Codex/Sol model-visible contextを品質維持のまま削減できるかを評価する。成立した場合だけ限定production adoption用の別Taskへ繋げる。

## External feasibility

status: observation
assumption: 外部Jev/RAG事例が示す狭い独立判定のbatch処理、usable-evidence基準のrelevance filtering、discarded positiveの監査、早期answerability gateが、本repositoryのBM25 navigation、review flow、source-proof境界でもQuality Deltaを維持しながらCodex/Sol消費を削減できるかは未検証。

## Contract

- 実行開始時にcurrent `glm-worker/internal/reposearch/`、worker/reviewer navigation、standalone parent repo-search、result limit / snippet / locator contract、telemetry ownerをcurrent Gitで再確認する。
- representativeなcurrent task / review / parent searchについて、filter前のcandidate数、後続source read / search回数、parent/model re-entry、model-visible bytesまたはbounded proxy、Codex/Sol usage、quality-relevant missをbaselineとして固定する。
- 最初はshadow evaluationに限定し、System-One出力へproduction pruning authorityを与えない。canonical source inspectionとSol/親判断をreferenceとしてfalse negative、coverage、Quality Deltaを測る。
- relevance判定は単なるtopic overlapではなく、「現在のsemantic objective / questionへ答えるためのusable evidenceとして候補を後段へ残す価値があるか」を対象にする。source correctnessそのものや最終finding有無をSystem-Oneへ委譲しない。
- candidateごとの独立判定を同一state上で行える場合は、複数の狭い質問を1回のbounded System-One callへbatchする案を優先評価し、candidateごとの追加model round-tripを増やさない。
- thresholdは外部記事値をそのまま採用せず、repository固有evidenceから決める。uncertain / provider failure / schema failure / incomplete outputはfail-openまたはSol側へ残し、削減成功として数えない。
- discarded candidateのうちcanonical判断で必要だったpositiveを監査可能にし、false negativeが見えない集計にしない。
- System-One追加cost / latency / GLM消費と、削減されたCodex/Sol input・実消費、source read、追加search、parent/model turnを同じcohortで比較する。追加costや補償readで相殺される場合はNo-Goとする。
- Goの場合だけ、適用範囲、fail-open条件、監視、rollback、canonical source-proof維持を定めた限定production adoption用の独立TaskをPlanへ登録する。

## Must not

- `repo-search-result-context-budget.md` または `repo-search-semantic-objective-query-separation.md` にSystem-One callを混ぜてzero-model-call contractを破らない。
- vector DB、Cloudflare Vectorize、embedding model、外部search API、generic RAG/retrieval frameworkをこのFindingの前提として導入しない。
- current BM25 / deterministic navigationを外部事例だけで置換しない。
- external benchmarkのaccuracy、latency、cost、削減率をrepository固有値として扱わない。
- relevance scoreだけでquality-required exhaustive proof、exact source inspection、reviewer independenceを省略しない。
- provider/schema failure、partial output、uncertain判定、discarded required evidenceをreduction成功へ計上しない。
- Jev/System-One confidenceだけを根拠にGLM/Sol model routingを変更しない。model routingは既存のBLOCKED taskとそのpermission / quality gateを維持する。
- shadow evaluator自体を作ることを成果にしない。実削減signalがなければNo-Goで終了する。

## Acceptance criteria

- representative cohortでbaselineとshadow filter後のcandidate retention、false negative、Quality Delta、追加System-One cost、後続read/search/model re-entry、Codex/Sol usageまたは比較可能なproxyが揃う。
- discarded candidateのcanonical positiveを監査でき、false negativeが閾値集計の外へ消えない。
- bounded batchとcandidate別callの必要性を比較し、余計なmodel round-tripを増やさない設計を選べる。
- filterによる削減が追加System-One cost、補償read/search、Quality Delta悪化で相殺される場合はNo-Goとする。
- Goの場合は限定production adoptionの独立Task、適用範囲、fail-open、monitoring、rollbackをtracked化する。
- No-Goの場合は既存deterministic repo-search tasksへscopeを逆流させず終了できる。

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。
- repo-search resultはnavigation locatorでありsource-code proofではない。
- Sol Highは曖昧・高risk・高レバレッジなsemantic tailへ集中させる。
- current schema / current ownerだけを正とし、旧shape維持のcompatibility layerを追加しない。

## Dependencies

none
