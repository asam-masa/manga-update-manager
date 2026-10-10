# ADR-0007: 差分レビューと重要変更の独立レビューを必須にする

- Status: Accepted
- Validation Status: Pending
- Date: 2026-10-10
- Related: [#38](https://github.com/asam-masa/manga-update-manager/issues/38)

## Context

Codexによる開発で、コミットされる内容の確認漏れと実装担当の思い込みを減らす。ユーザーはコミット前の差分レビューと重要変更の敵対的検証を希望し、実行方式、対象基準、独立担当の利用と上限を承認した。

対象は開発手順とリポジトリ内のスキルである。アプリの動作、Git hook、CI、PRのマージは対象外とする。

## Decision Drivers

- 小規模な開発で継続できる負担
- 実際にコミットする内容とレビュー対象の一致
- 重要変更に対する独立性と一次根拠
- 権限不足や未解決の指摘を黙って省略しないこと

## Considered Options

### AGENTS.mdとスキルによる作業手順

- 利点: リポジトリで共有でき、AI起動の自動化を追加せず始められる。
- 欠点: 通常のPowerShellからの手動コミットを機械的には止めない。
- リスク: 手順の遵守が必要で、レビュー対象が変わると結果を見直す必要がある。

### Git hookで実行する

- 利点: Git操作時のチェックに結び付けられる。
- 欠点: AI起動、権限、待ち時間、利用不能時の処理が追加で必要になる。
- リスク: 各環境への設定が必要で、hookだけを安全性の保証にはできない。

### 全変更に独立レビューを行う

- 利点: 適用判断の漏れを減らせる。
- 欠点: 小さな変更にも実行時間と検証負担がかかる。
- リスク: 形式的な確認が増え、重要な問題へ集中しにくい。

## Decision

AGENTS.mdでCodexのコミット前・PR前の差分レビューを必須にする。重要変更では独立したサブエージェントによる敵対的レビューを追加する。Git hookは導入しない。

重要変更の現在の基準と実施手順は[コードレビュー観点](../develop/quality/code-review.md)とスキルを正本とする。独立担当は原則1名、重大な指摘で判断が割れた場合のみ追加1名、修正後の再確認は1回までとする。再委譲はしない。実装担当が一次根拠と反証を確認する。

## Consequences

コミット前はindexの内容、PR前は固定したコミット範囲を確認する。対象の版、実施状況、未実施、残るリスクを記録する。未解決の`[must]`または必要な独立レビューの実行不能時は停止して相談する。

レビューは正しさを保証するものではなく、テストや利用者による実機確認を置き換えない。手動コミットまでの強制が必要になった場合は、別Issueでhookを検討する。アプリのデータ形式と既存データへの影響はない。

## Validation

両スキルはskill-creatorの構造検証に成功した。対象版と文書参照を確認し、この変更自身のステージ済み差分で通常レビューと独立担当1名のレビューを実施した。独立担当から修正必須の指摘はなかった。

独立担当が版の確認で`git write-tree`を誤って試し、権限拒否で失敗した。書き込みは成立していない。この実行結果に基づき、tree ID取得は親に限り、独立担当は渡されたtree IDとの一致を読み取り確認することを明記した。

アプリコード変更がないため、アプリのテスト・ビルド・実機確認は今回実施しない。構造と初回レビューの確認は完了したが、将来の重要変更での継続運用、重大な指摘の対立、独立レビュー不能時の停止は未検証なのでPendingを維持する。

## References

- [参考: review-code-changes](https://github.com/asam-masa/browser-launcher/tree/main/.agents/skills/review-code-changes)
- [参考: run-adversarial-review](https://github.com/asam-masa/browser-launcher/tree/main/.agents/skills/run-adversarial-review)
- [コミット手順](../develop/workflow/commit.md)
- [PR・レビュー運用](../develop/workflow/pull-request-review.md)
