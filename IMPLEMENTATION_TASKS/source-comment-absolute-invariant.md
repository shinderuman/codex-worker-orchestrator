# Task: source commentをabsolute machine invariantで全面禁止する

## Original instruction

`````text
# 最優先割り込みTask：source commentを全面禁止し、machine lintで再発不能にする

現在、GLMの5h limitによって既存ACTIVE taskが停止しており、working treeにはそのtaskの未commit実装が存在する。

まず現在ACTIVE taskを既存contractどおり安全にresumeし、

- implementation
- required verification
- independent review
- 必要なSol/Codex品質gate
- task completion metadata同期
- commit

まで正常完了すること。

現在taskのworking treeへ本件を混ぜない。
本件を理由に現在taskをrollback・中断・scope拡張しない。

ただし現在taskのcommit完了直後、本件を既存Plan上のすべての通常NEXT taskより優先して、
最上位priorityの独立taskとしてACTIVE化すること。

今回はpriority判断を親Codexへ委ねない。
ユーザー明示指定として、本件を現在task完了直後の最優先taskとする。

---

# 目的

GLM worker/reviewerがsource codeへ大量の冗長commentを生成し、
過去に一度大幅削除とcomment規則強化を行ったにもかかわらず、
その後再びcommentが増殖した。

本件は単なるstyle cleanupではない。

以前の対策では、

- commentは原則書かない
- コードの言い換えは禁止
- 非自明な理由・不変条件・security・外部仕様等のみ例外
- 実装完了前に新規・変更commentを削除判定
- reviewerもcomment品質を確認

というsemantic ruleを導入した。

しかし実ログ上、GLMはそのruleを実際に読んでいるにもかかわらず、

- 非自明な不変条件
- 実装理由
- 重要前提
- compatibility
- safety

等の例外を広く解釈してcommentを書き続けた。

reviewerも同種のモデルであるため、
workerが「必要なcomment」と判断したものをreviewerも「必要」と判断し得る。

さらに一度commitされたcommentは後続taskでは既存commentとなり、
新規・変更comment中心のreviewから外れやすく、
comment量が一方向に蓄積する構造になっていた。

したがって今回、

> モデルに「良いcomment / 悪いcomment」を判断させる方式

そのものを廃止する。

目的は、

> source code内の自然言語comment禁止をmachine-checkableなabsolute invariantへ変換し、
> GLMの判断能力・遵守能力を信用しなくても再発できない状態にする

ことである。

単にpromptを強くするだけで完了してはならない。

---

# 最重要：本TaskはGLMへ委譲しない

本Taskでは以下をすべて親Codex自身が行うこと。

- 既存履歴・現在ruleの確認
- escaped原因の確認
- comment rule修正
- repository source inventory
- 既存linter調査
- linter設計
- linter実装
- linter test
- `--fix`実装
- repository全体への初回適用
- 既存comment一掃
- comment削除後のsemantic確認
- verification
- machine gate integration
- 最終review
- commit

GLM workerへ実装させない。
GLM reviewerへreviewさせない。
GLMへ既存commentの必要性を判定させない。
GLMへcomment cleanupを依頼しない。

今回のmachine gateとclean baselineが成立しcommitされるまで、
本件に関してGLMを品質保証主体として利用しない。

これはGLMの恒久利用禁止ではない。

本Task完了後は通常のGLM orchestrationへ戻してよい。

その後はGLMがcommentを書いてもmachine gateで拒否されるため、
comment品質についてGLMを信用する必要がない状態にする。

---

# 1. 過去対策とescaped原因を一次証拠で確認する

過去のcomment cleanupとcomment rule導入commitをGit履歴から特定し、
現在までにcommentが再増殖した経路を確認する。

少なくとも以下を一次証拠で確認すること。

- 大幅なcomment削除を行ったcommit
- comment ruleを強化したcommit
- そのruleの実内容
- worker/reviewerへそのruleがproductionで渡っていたか
- rule導入後にGLMがcommentを追加した実例
- reviewerがそれを拒否しなかった実例
- comment量をrepository-wideに固定するmachine postconditionが存在しなかったこと

原因を、

> GLMがruleを読んでいなかった

と根拠なく単純化しない。

現時点で想定している主要原因は、

1. semantic exceptionの判定をモデルへ委ねた
2. worker/reviewerが同じ傾向で例外を許容できた
3. 一度commitされたcommentが後続reviewで再評価されにくかった
4. clean baselineをrepository-wide absolute machine invariantとして固定していなかった

ことである。

一次証拠と異なる事実が見つかった場合は、事実を優先して原因分析を修正する。

---

# 2. comment ruleを全面禁止へ変更する

現在のcomment ruleにある、

- 原則禁止
- 非自明なら許可
- securityなら許可
- invariantなら許可
- compatibilityなら許可
- 外部仕様なら許可
- 一見不要な処理を残す理由なら許可

等の、モデルによるsemantic exceptionを撤去する。

新contractは言語非依存で単純にする。

## Contract

repository内でlint対象となるsource codeでは、
自然言語commentを禁止する。

以下はすべて禁止対象である。

- コードの説明
- 関数の説明
- type/class/package/moduleの説明
- exported APIのdoc comment
- testのsetup説明
- Arrange / Act / Assert説明
- test意図の説明
- 実装理由
- 設計理由
- 非自明な不変条件
- security上の説明
- compatibility説明
- workaround説明
- external specification説明
- 「なぜこの処理が必要か」の説明
- 一見不要な処理を残す理由
- TODO
- FIXME
- NOTE
- XXX
- section separator
- history comment
- temporary comment
- debug comment
- lint suppression comment
- formatter suppression comment
- analyzer suppression comment

モデルが、

> これは重要だから例外
> 将来の誤変更防止に必要
> このcommentがないと理解しにくい

等と判断して残すことを認めない。

ユーザーが例示した、

https://github.com/shinderuman/codex-worker-orchestrator/blob/b8a6d7d20002152a50d3cc2c8d35e53ea06d5966/glm-worker/internal/app/multirepo_process_test.go#L351
の該当箇所のようなcommentも禁止対象とする。

test sourceも例外ではない。

Goだけを対象にしてはならない。
shellやその他のsource languageへcommentが逃げる余地を残さない。

---

# 3. commentへ保存していた重要情報の扱い

comment禁止によって本当に失わせてはいけない意味情報が存在する場合は、
commentを復活させるのではなく必要に応じて、

- naming
- type
- state representation
- function decomposition
- executable test
- production invariant
- tracked task contract
- IMPLEMENTATION_RULES
- その他既存の適切なtracked contract surface

へ移す。

ただし、

> commentが消えるから全部refactorする

という大規模cleanupにはしない。

commentを削除してもcode semantics・test semantics・contract理解が変わらないものは単純に削除する。

意味移植が必要なものだけ親Codexが個別に判断する。

---

# 4. repository内のsource種別をinventoryする

comment lint設計前にtracked repositoryを確認し、
現在存在するfile種別を分類する。

最低限、

- production source
- test source
- executable scripts
- build/install scripts
- configuration source
- generated source
- documentation
- requirement/Plan/History
- fixtures/testdata
- plain data

を区別する。

comment禁止対象は「source code / executable or parsed source」である。

Markdown等の、

- README
- requirement document
- IMPLEMENTATION_PLAN
- IMPLEMENTATION_TASKS
- IMPLEMENTATION_RULES
- IMPLEMENTATION_HISTORY
- EVAL documentation

の本文はsource commentではないため、本lintによる自然言語禁止対象にはしない。

JSON等、そもそもcomment syntaxを持たないdata形式も対象外。

fixture/testdataは内容を確認する。

sourceとして実際にparse/executeするfixtureは対象になり得るが、
単なる外部入力sampleまで拡張子だけで一律comment禁止扱いしない。

---

# 5. Go限定にしない

本件はGo comment style問題ではない。

GLMがsourceへ自然言語説明を生成する挙動そのものを封じる。

したがってlinterはrepositoryで現在利用しているすべてのcomment可能source languageを対象とする。

少なくともrepository現物を確認し、

- Go
- shell
- その他存在する実装言語

を対象化すること。

Goだけcleanにしてshell等へ冗長commentを移動・増殖させる状態は禁止。

---

# 6. 将来の新source種別も黙って抜けないようにする

現在存在する拡張子だけをhardcodeして、

> 未知のsource fileは全部黙ってskip

という設計にしない。

将来repositoryへ新しいsource language/script種別が追加された際、
comment lint coverageから無言で脱落しないようにする。

ただし巨大なuniversal language detectorは作らない。

現在のrepository構造に対して最小限、

- tracked source分類
- known source kinds
- explicit ignored document/data kinds
- 未分類source候補の検出

等でfail closedまたは明示分類を要求できる構造を検討する。

目的は、

> GLMが新しいscript fileを追加したら、その拡張子だけcomment lint対象外だった

という逃げ道を防ぐことである。

---

# 7. 唯一の例外はmachine-semantic syntax

comment syntaxを利用しているが、
compiler / interpreter / build tool / OS等が直接解釈し、
削除すると実行・build semanticsが変化するものだけを例外とする。

例として、

- shebang
- Go toolchain directive
- compiler directive
- generator directive
- 実際のrepositoryで必要なmachine-readable directive

等があり得る。

ただし重要なのは、

> machine semanticを持つかどうかをモデルが毎回判断する

方式にしないことである。

allowlistはlinter側にexactに定義する。

方針:

- 現repositoryで実際に必要なものだけ許可
- broad patternで「directiveっぽいもの」を許可しない
- 将来用に大量のdirectiveを先回りallowlistしない
- 現在利用されていないものは原則追加しない
- 必要な新directiveが将来発生したらlinter変更として明示reviewする

lint suppressionはmachine-semantic exceptionとして認めない。

例えば、

- `//nolint`
- `//lint:ignore`
- `//revive:disable`
- shellcheck suppression
- その他source側からcheckerを無効化するcomment

は原則禁止対象とする。

本commentlint自身をsource commentでdisableする仕組みを作らない。

---

# 8. 既存linterをまず確認する

新規実装前に、
現在repositoryで利用中または標準的なlint ecosystemに、

> repository内の通常commentを全面禁止し、
> machine-semantic directiveだけをexact allowlistできる

既存rule/toolが存在するか確認する。

要求を正確かつ単純に満たす既存toolがあるなら利用してよい。

ただし既存toolを使うために、

- 大きなdependencyを追加
- plugin frameworkを追加
- 複雑な設定を大量記述
- semanticな「良いcomment判定」を残す
- source-side suppressionを広く許可
- repository全sourceを扱えない

状態になるなら採用しない。

今回のcheckerは本質的に小さい。
既存tool調査だけに過剰な時間を使わない。

適切な既存toolがなければrepository専用linterを自作する。

---

# 9. repository専用comment linterの要件

自作する場合は外部依存を極力増やさず、
既存dependencyまたは各言語の標準parser/scannerを第一候補とする。

外部interfaceは単一のcomment lint commandへ収束させる。

例:

    commentlint
    commentlint --fix

名称はrepository既存命名規約に合わせてよい。

薄いentrypoint + internal implementationという既存構成に従い、
本件だけの巨大frameworkを作らない。

machine outputはrepositoryの既存machine-output contractへ合わせる。

---

# 10. 単純grepは禁止

以下のような実装は禁止。

    grep '//'
    grep '#'
    grep '/*'

理由は、

- string literal
- URL
- regex
- shell parameter expansion
- quoted `#`
- heredoc content
- generated content

等を誤検出するため。

language-awareにcomment tokenを認識する。

対象言語ごとに、

1. 標準parser/scanner
2. repositoryですでに利用しているparser
3. 必要最小限の決定論的lexer

の順で単純な方法を選ぶ。

巨大なmulti-language parsing frameworkは作らない。

---

# 11. Goの検出

GoについてはGo標準libraryを使い、
actual comment tokenをparse/scannerで検出する。

最低限、

- `// normal comment`
- `/* block comment */`
- doc comment
- test comment

を禁止として検出する。

string literalやraw string内のcomment markerは誤検出しない。

machine-semantic directiveだけexact allowlistする。

---

# 12. shellの検出

shellについても単純な`#` grepを使わない。

最低限、

- shebang
- standalone comment
- inline comment
- quoted `#`
- `${var#pattern}`
- `${var##pattern}`
- heredoc
- command argument内の`#`

等を区別する。

利用可能な既存parserがrepository/環境に存在し、依存追加が合理的なら使用を検討してよい。

なければ現在repositoryで使用しているshell syntaxに対して、
必要最小限で決定論的なscannerを実装する。

安全に自動判定できないsyntaxを無理に誤parseするよりfail closedを優先する。

---

# 13. その他のsource language

repository inventoryでGo/shell以外のcomment可能sourceが存在する場合、
同様にlanguage-awareな検査を実装する。

その言語だけcommentlint対象外として放置しない。

ただし存在しない言語のparserを将来用に先回り実装しない。

現在repositoryに必要なものだけ対応する。

---

# 14. `--fix`を実装する

linterには`--fix` modeを持たせる。

通常mode:

    commentlint

- repository対象source全体を検査
- 禁止commentを報告
- 1件以上ならnon-zero
- sourceを変更しない

fix mode:

    commentlint --fix

- 禁止commentだけを削除
- allowed machine-semantic directiveを保持
- semantic codeを変更しない

`--fix`は賢いrepair toolにしない。

自動で以下を行わない。

- rename
- refactor
- function split
- test追加
- logic変更
- comment内容のコード化
- contract書換え
- 「重要commentだけ残す」判断

これは決定論的なcomment削除器である。

---

# 15. `--fix`は安全性を優先する

comment削除によってsyntaxが壊れる可能性がある場合、
誤ったrewriteよりfail closedを優先する。

自動安全削除できないcaseは、

- lint violationとして残す
- 親Codex自身がsourceを修正
- 再lint

とする。

GLMへ判断を戻さない。

fix後は各言語の標準formatterが既存workflowに存在する場合、
必要な整形だけ行う。

---

# 16. `--fix`は冪等にする

同じrepositoryへ、

    commentlint --fix
    commentlint --fix

と2回実行した場合、
2回目は変更なしになること。

fix後には必ず、

    commentlint

が0 violationsとなること。

---

# 17. linter自体を十分にtestする

最低限以下をfixture/testで固定する。

## 必ずFAIL

- 普通の1行comment
- 複数行comment
- block comment
- function説明
- type/class/package説明
- doc comment
- exported API doc
- test setup説明
- Arrange/Act/Assert説明
- implementation rationale
- invariant説明
- security説明
- compatibility説明
- workaround説明
- TODO
- FIXME
- NOTE
- XXX
- section separator
- history comment
- lint suppression
- formatter suppression
- ユーザー提示の`multirepo_process_test.go`型comment

## 必ずPASS

- commentのないsource
- string literal内のcomment marker
- raw string内のcomment marker
- shell quoted `#`
- shell parameter expansionの`#`
- repositoryで実際に必要なmachine-semantic directive
- shebang

## `--fix`

- 禁止commentだけ消える
- code token/semanticsを変えない
- allowed directiveは残る
- fix後lint PASS
- 2回目fixがno-op
- 複数commentが全て消える
- block/inline等、各対応言語の実際のcomment形式で成立する

---

# 18. 初回cleanupは親Codex自身が行う

linter完成後、
親Codex自身がrepository全対象sourceへ`--fix`を適用する。

GLMにcommentを1件ずつ判定させない。

GLMへ、

> 必要なcommentだけ残せ

と依頼しない。

machine lint contractに従い、
禁止commentは原則すべて削除する。

---

# 19. 全sourceを薙ぎ払う

初回cleanupはGo限定ではない。

lint対象となるrepository内すべてのsourceについて、

    forbidden natural-language comments = 0

を成立させる。

対象にはproduction codeだけでなく、

- tests
- scripts
- installer
- smoke tests
- tools
- helper source

も含む。

ユーザー提示のようなtest内commentも残さない。

---

# 20. comment削除後に親Codexがsemantic reviewする

大量削除後、
親Codex自身がdiffを確認する。

確認するのは、

> commentが本当に必要だったか

ではなく、

> comment削除によってcode/test/contract上の意味や安全性が失われていないか

である。

意味が失われた箇所だけ、

- executable test
- naming
- explicit state
- tracked contract

等へ必要最小限移す。

comment自体を戻すことを解決策にしない。

---

# 21. repository-wide absolute invariantにする

今回のlintは、

> 前回commitから新しく増えたcommentだけを見る

方式にしない。

毎回repository全対象sourceを検査する。

最終contractは、

    forbidden comments == 0

というabsolute invariantである。

これにより何らかの経路で違反commentが入り込んでも、
次回machine verificationで必ず検出できる状態にする。

---

# 22. 通常verification chainへ統合する

commentlintを作成しただけでは完了しない。

今後の通常task lifecycleで、
comment違反が存在したまま正常完了・commitへ進めないよう、
既存machine verification chainへ統合する。

現在repositoryに存在する、

- test
- lint
- build
- install preflight
- commit-ready verification
- self-protection
- その他mandatory gate

を調べ、
最小で適切な既存箇所へ接続する。

---

# 23. 「GLMに実行させる」だけを対策にしない

worker promptへ、

> 最後にcommentlintを実行すること

と書くだけでは不十分。

GLMが実行を忘れても正常完了できないmachine pathへ入れる。

期待する状態:

    GLMがcommentを追加
        ↓
    existing mandatory verification
        ↓
    commentlint
        ↓
    FAIL
        ↓
    comment除去なしではtask completion不可

instruction complianceではなくmachine postconditionで保証する。

---

# 24. 過剰な重複実行は避ける

machine gateはmandatoryにするが、
同じcommentlintを1つのscenario内で無意味に何十回も実行する必要はない。

既存verification architectureを確認し、
coverageを維持しながら最小の必要箇所へ統合する。

今回別途問題になっている反復machine costを増やさないよう注意する。

ただし高速化目的でgate自体をoptionalにしてはならない。

---

# 25. production-path negative verification

少なくとも以下をmachineで確認する。

1. Go production sourceへ通常comment追加 → FAIL
2. Go testへ通常comment追加 → FAIL
3. shell production/scriptへ通常comment追加 → FAIL
4. shell testへ通常comment追加 → FAIL
5. doc comment追加 → FAIL
6. TODO/FIXME追加 → FAIL
7. lint suppression追加 → FAIL
8. quoted/comment-marker string → PASS
9. shebang → PASS
10. required machine directive → PASS
11. allowed directive以外の「directive風comment」 → FAIL
12. lint対象全体がcleanならPASS

Go/shell以外の現在repository source languageが存在する場合、
同等のnegative caseを追加する。

---

# 26. unknown source coverageも確認する

新しいsource-like tracked fileがcommentlint非対応のまま追加された場合に、
黙ってcoverageから脱落しないことをtest/contractで確認する。

ただしdocument/dataまで全部failさせる雑な方式にはしない。

現在のrepository分類に基づく明確なsource-vs-nonsource contractにする。

---

# 27. 旧comment ruleを簡潔化する

machine gate成立後、
worker/reviewer instructionに残っている旧comment品質規則を整理する。

削除するもの:

- 非自明なら許可
- invariantなら許可
- securityなら許可
- compatibilityなら許可
- external specなら許可
- reviewerがcommentの有用性を判断する
- 「迷えば書かない」等のsemantic判断

最終ruleは言語非依存で概ね以下へ収束させる。

> lint対象sourceの自然言語commentは禁止する。
> worker/reviewerはcommentの必要性を独自判断して例外化してはならない。
> machine-semantic syntaxとして許可されるものはcommentlintのexact allowlistを唯一の正とする。
> commentlint violationは修正しない限りtaskを完了できない。

同じ内容を複数promptへ長文複製しない。
既存のsingle contractへ最小統合する。

---

# 28. reviewerにcomment品質判断をさせない

本Task完了後、
GLM reviewerに、

> このcommentは有益か
> このcommentは非自明か
> 残す価値があるか

を判断させない。

commentについてはmachine lint結果だけをauthorityとする。

違反なら修正対象。
違反でなければcomment品質review自体を行わない。

---

# 29. escaped原因をHistoryへ残す

本件をstyle cleanupだけとしてHistoryへ記録しない。

最低限、簡潔に以下を残す。

- 過去にcommentを大幅削除した
- semanticな「原則禁止＋限定例外」ruleを導入した
- GLMはruleを実際に読んでいた
- それでもモデルが例外を広く解釈した
- reviewerもsemantic exceptionを許可できた
- commit済みcommentが後続reviewから逃げやすかった
- repository-wide absolute machine invariantがなかった
- その結果commentが再増殖した
- 今回semantic judgmentを廃止しmachine comment lintへ置換した

これを新しい巨大な原因分析文書にしない。
既存Historyの適切なescaped cause記録へ圧縮して追加する。

---

# 30. linter自身のscopeを過剰化しない

本件で作るのはcomment禁止checkerである。

以下を追加しない。

- generic lint framework
- plugin architecture
- universal parser framework
- arbitrary policy engine
- generic code quality platform
- AST rewrite framework
- comment quality classifier
- AI comment classifier
- semantic comment migration framework

単一責務を守る。

---

# 31. `--fix`をsemantic migration toolにしない

`--fix`は、

> 禁止commentを安全に削除する

だけ。

comment内の文章を解析して、

- testへ変換
- identifierへ変換
- documentationへ移動
- requirementへ自動コピー

等を行わない。

重要情報の移植判断が本当に必要な少数箇所だけ親Codex自身が行う。

---

# 32. clean baseline完成までGLMを戻さない

以下が成立するまで本Taskを完了扱いにしない。

- linter contract確定
- linter tests green
- repository全source cleanup
- forbidden comment 0
- post-cleanup semantic review
- normal verification green
- machine gate integration
- negative production-path verification
- final clean tree
- commit

その後に通常のGLM orchestrationへ戻す。

---

# 33. verification

comment一掃後、repository現物に応じて最低限、

- commentlint
- 各言語formatter
- Go gofmt
- Go test
- Go race
- Go vet
- Go build
- shell syntax validation
- install smoke
- repository既存の関連integration/smoke
- machine gate negative tests
- clean working tree / final diff確認

を行う。

既存repository contractでさらにrequired verificationがある場合はそれにも従う。

testを省略してcomment cleanup完了としない。

---

# 34. 完了条件

本Task完了には以下すべてが必要。

- 先行ACTIVE taskが先に正常完了・commit済み
- 本件をその直後の最優先独立taskとして実施
- 本件diffを先行taskへ混ぜていない
- 本TaskではGLM workerを利用していない
- 本TaskではGLM reviewerを利用していない
- 過去comment cleanup/rule導入履歴を一次証拠で確認
- escaped原因を確認
- semantic comment exceptionを撤去
- ruleを言語非依存へ変更
- repository source inventory完了
- Go限定ではないlint coverage
- 既存linter有無を確認
- 必要なら最小repo専用commentlint実装
- language-aware comment detection
- grep-only判定ではない
- exact minimal machine-semantic allowlist
- source-side suppression不可
- `--fix`実装
- `--fix`がcomment削除だけを行う
- `--fix`が冪等
- linter unit/integration tests
- unknown source coverageのfail-open回避
- repository全lint対象sourceへ初回適用
- forbidden natural-language comments = 0
- production source comment = 0
- test source comment = 0
- script comment = 0
- ユーザー提示型comment = 0
- lint再実行0 violations
- comment削除後の親Codex semantic review
- 必要な意味情報だけcode/test/tracked contractへ移植
- mandatory machine verification chainへ統合
- GLMがcomment追加したnegative caseが実際にFAIL
- formatter/test/race/vet/build/shell/install smoke等required verification green
- escaped原因をHistoryへ記録
- final diff確認
- clean tree
- 親Codex自身による最終review
- 通常task lifecycleに従ってcommit

---

# Must not

- 現在停止中のACTIVE taskへ本件を混ぜない
- 現在taskを本件のためにrollbackしない
- 現在task完了前に本件を実装開始しない
- 現在task完了後に本件を通常priorityへ戻さない
- 本TaskをGLM workerへ委譲しない
- 本TaskをGLM reviewerへ委譲しない
- 「もっと強くcomment禁止と指示する」だけで終了しない
- Goだけを対象にしない
- production codeだけを対象にしない
- testを例外にしない
- shellを例外にしない
- doc commentを例外にしない
- 理由commentを例外にしない
- invariant commentを例外にしない
- security commentを例外にしない
- external spec commentを例外にしない
- TODO/FIXMEを例外にしない
- lint suppressionを例外にしない
- comment数thresholdで妥協しない
- 「以前より減った」で完了しない
- diff追加commentだけを検査しない
- baseline comparisonだけにしない
- model判断でallowlistを広げない
- broad directive patternを許可しない
- 将来用directiveを大量allowlistしない
- sourceからcommentlintをdisableできるようにしない
- grepだけでcommentを判定しない
- unknown sourceを黙ってskipして逃げ道を作らない
- document/dataを雑にsource扱いしない
- linterのために巨大frameworkを作らない
- unnecessary dependencyを追加しない
- `--fix`へsemantic refactorをさせない
- `--fix`で安全に消せないcommentを無理に書換えない
- comment削除を口実に無関係なrefactorを行わない
- instruction complianceだけを再発防止証拠にしない
- GLMのPASSを本Taskの品質証拠にしない
- forbidden commentが1件でも残った状態で完了しない

---

# 最終原則

今回の目的は、

> GLMに良いcommentだけを書かせる

ことではない。

それは過去の実運用で成立しなかった。

今後は、

> source codeを自然言語commentの保存場所として使用させない

ことをrepository invariantとする。

重要な意味はcode / test / tracked contractで保持し、
commentについてモデルの自己判断を不要にする。

最終状態では、

    repository source
        ↓
    forbidden natural-language comment = 0
        ↓
    mandatory machine lint
        ↓
    違反があればtask completion不可

を恒久contractとして成立させること。
`````

## Amendments

none

## Resolved references

- 「現在ACTIVE task」は、受領時点の `IMPLEMENTATION_TASKS/009-worker-call-outliers.md` を指す。
- 「既存Plan上のすべての通常NEXT taskより優先」は、Task 009完了・commit直後に本taskをACTIVEへ昇格し、`install-smoke-loop-cost-reduction.md`を含む他のNEXTより先に実施する意味と解決した。
- ユーザー提示のGitHub例は、commit `b8a6d7d20002152a50d3cc2c8d35e53ea06d5966` の `glm-worker/internal/app/multirepo_process_test.go` 351行付近を指す。
- 「本TaskはGLMへ委譲しない」は、worker・reviewer・cleanup・実装・verification・最終reviewの全工程を親Codexが直接実施する明示的な直接編集・直接実行許可である。

## External feasibility

status: not-applicable

## Purpose

source code内の自然言語commentをmachine-checkableなrepository-wide absolute invariantで全面禁止し、モデルのsemantic例外判断に依存する再発経路を廃止する。

## Contract

- Task 009を正常完了・commitしてから本taskを最優先ACTIVEへ昇格する。
- 本taskの全工程を親Codex自身が行い、GLM worker/reviewerを利用しない。
- repository内のsource種別をinventoryし、Go・shell・現在存在する他のcomment可能sourceをlanguage-awareに検査する。
- machine-semantic syntaxだけをexact allowlistで許可し、自然言語comment・doc comment・suppression comment等は例外なく禁止する。
- repository専用の最小comment linterに通常検査と安全・冪等な `--fix` を用意し、unknown source候補を黙ってskipしない。
- 初回cleanupで全lint対象sourceを0 violationsへ収束させ、意味が失われる箇所だけcode・test・tracked contractへ移す。
- linterをmandatory machine verification chainへ最小統合し、instruction complianceではなくabsolute postconditionで完了を拒否する。
- 過去対策とescaped原因を一次証拠で確認し、圧縮してHistoryへ記録する。

## Must not

- Task 009へ混ぜる、rollbackする、scope拡張する、またはTask 009完了前に実装を開始しない。
- 本taskをGLM worker/reviewerへ委譲しない。
- semantic comment exception、threshold、diff-only検査、grep-only検出、broad directive allowlist、source-side suppressionを残さない。
- Go・production sourceだけへ限定せず、test・shell・現在の他sourceを例外化しない。
- unknown source候補をfail-openで黙って除外せず、document/dataを雑にsource扱いしない。
- generic lint/parser/policy frameworkや不要dependencyを追加しない。
- `--fix`でcomment削除以外のsemantic refactor・migrationを自動化しない。
- 品質gate・acceptance criteriaを弱めず、GLMのPASSを本taskの品質証拠にしない。

## Acceptance criteria

- Original instructionの全完了条件を満たす。
- 過去cleanup/rule導入・production prompt配線・escaped追加例・review gap・machine postcondition欠如を一次証拠で確認する。
- source inventoryと明示的な対象/非対象分類を固定する。
- language-aware detector、exact minimal directive allowlist、安全で冪等な `--fix`、unknown source coverage testを実装する。
- repository全lint対象sourceでforbidden comment 0を達成する。
- production-path negative cases、linter unit/integration、formatter、Go test/race/vet/build、shell syntax、install smoke、既存required verificationがgreenである。
- comment削除後のsemantic reviewとfinal diff reviewを親Codex自身が行う。
- machine gateがGLMのcomment追加を拒否することを確認する。
- escaped原因をHistoryへ記録し、task completion metadata同期後に親Codexがcommitする。
- final HEADでclean treeとなり、通常GLM orchestrationへ戻せる。

## Historical invariants

- 最上位目的は、Sol High相当の品質を可能な限り維持しながらCodex/Sol実消費量を大幅に削減すること。
- Task 009は`--call-outliers`の実装・全verification・独立review・親採用・commitを完了し、本taskの実装はその完了後に開始する。

## Dependencies

none

## Review findings

none

## Current boundary

Task 009完了・commit後の最優先ACTIVEへ昇格済み。本taskの実装は未着手で、GLMを使わず親Codexが直接開始する。
