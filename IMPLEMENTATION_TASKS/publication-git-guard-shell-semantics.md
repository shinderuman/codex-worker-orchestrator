# Task: publication Git guard shell semantics

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- External Reviewでpublication PreTool Git bypass判定がshell lexical semanticsを十分扱わず、隣接quoted/unquoted literalの`--no-"verify"`が実shellでは`--no-verify`になる形を安全に分類できないことを確認した
- shell variable expansion等のdynamic formも、実行されるGit operationを静的に安全分類できない場合がある
- failed publication umbrellaへこのcommand-classification責務まで吸収されたため、publication sequence本体から独立させる

## Purpose

publication guardのGit operation classificationをshell semanticsに対してfail closedにし、guard bypass判定だけを独立したreview可能責務として固定する。

## External feasibility

status: not-applicable

## Contract

- adjacent quoted/unquoted literalを実shell token semanticsに従って正規化し、`--no-"verify"`相当を`--no-verify`として分類する
- staticに安全なGit argvへ解決できる形だけを既存operation classifierへ渡す
- variable expansion、command substitution、dynamic concatenation等で実operationを安全に一意分類できない場合はguard通過へ縮退せずfail closedする
- existing allowed publication Git operationsとparent-only remote write boundaryを維持する
- classification errorはmachine-readableなbounded failureとして扱う

## Must not

- shell文字列の単純substring blacklistだけで解決しない
- arbitrary shellを実行してclassification結果を得ない
- dynamic formを「たぶん安全」と推測して許可しない
- hook install/promotion/completion owner等の別publication責務を本taskへ含めない

## Acceptance criteria

- quoted/unquoted concatenationで実argvが`--no-verify`になるfixtureを確実に拒否する
- dynamic expansionでoperationを安全分類不能なfixtureがfail closedする
- canonicalに安全なGit publication commandsは既存どおり許可される
- shell lexical edge caseの追加でparent-only remote writeやexisting guard semanticsを弱めない
- relevant guard tests、Repository Lint、必要なfull Go suiteがPASSする

## Historical invariants

- publication guardは実際にGitへ渡るoperation semanticsを基準に判断する
- 分類不能は許可ではなくfail closedである

## Dependencies

none
