# ADR運用ガイド

## 目的

Architecture Decision Record（ADR）は、重要な設計判断の背景、候補、採用理由、影響、検証方法を記録するために使用します。結論だけでなく、後から判断を再検討できる根拠を残します。

## 作成する基準

次のいずれかに該当する場合はADRを作成します。

- 複数の妥当な候補があり、選択による影響が大きい
- 複数の機能、外部境界、データ、運用へ影響する
- 制約やトレードオフを後から参照する必要がある
- 既存ADRのDecisionを置き換える

局所的で容易に変更できる実装詳細は、コードコメントまたはPR本文へ記録します。

## 決定までの手順

1. 解決する問い、対象範囲、対象外、制約を明確にする
2. 関連する要件、既存ADR、コード、設定を確認する
3. 外部仕様は公式文書などの一次情報で確認する
4. Decision Driversを定める
5. 複数候補を同じ基準で比較する
6. 利点、欠点、リスク、検証方法を整理する
7. 推奨案と根拠を提示する
8. 選択後にDecisionを確定する

承認前の推奨案は提案であり、Decisionではありません。

## ファイルと番号

- ADRは`docs/adr/`へ保存する
- ファイル名は`NNNN-short-description.md`とする
- ADRを作成するIssueで番号を確保する
- 番号を確保するときは、`main`にある既存ADRの最大番号と、作業中Issueで予約済みの番号を確認する
- ADRを削除して番号を再利用しない
- 置き換えられたADRも履歴として残す

## Status

| Status | 意味 |
| --- | --- |
| Proposed | 検討中 |
| Accepted | 採用済み |
| Superseded | 後続ADRに置き換えられた |
| Deprecated | 現在は推奨しないが置き換え先がない |
| Rejected | 検討したが採用しなかった |

## Validation Status

Decisionの採否と実機・運用上の検証状態を分けて記録します。

| Validation Status | 意味 |
| --- | --- |
| Pending | 検証前または検証中 |
| Verified | 記載した検証を完了した |
| Failed | 検証で前提または期待を満たさなかった |
| Not Applicable | 別途検証を必要としない |

## 必須構成

新しいADRは[ADRテンプレート](../../templates/adr.md)を使用します。

- `Context`: 背景、問い、範囲、制約、確認済みの事実
- `Decision Drivers`: 候補を比較する判断基準
- `Considered Options`: 各候補の利点、欠点、リスク
- `Decision`: 採用した候補と理由
- `Consequences`: 利点、負担、移行、運用、後続作業
- `Validation`: 判断を確認する方法と結果
- `References`: 一次情報と関連資料

参照資料がない場合も`References`を省略せず、「なし」と記載します。

