# ADR-0016: Wailsのエラー包装を越えて安全なAPI情報を保持する

- Status: Accepted
- Validation Status: Verified
- Date: 2026-10-20
- Related: [#181](https://github.com/asam-masa/manga-update-manager/issues/181)、[#180](https://github.com/asam-masa/manga-update-manager/issues/180)

## Context

Goは安全な`APIError{code,message}`を返し、画面はその形を期待していた。実SQLiteのCommit競合を使ったWebView2検証では、結果不明の専用案内が汎用エラーになった。採用版Wails v2.16.0のCallbackは`new Error(message.error)`で包装するため、オブジェクトの既定文字列化でcode/messageを失う。構造化オブジェクトを直接rejectする画面テストではこの経路を再現できなかった。

対象は共通のエラー伝達境界である。成功DTO、DB、保存形式、ドメインの結果分類、Wails本体・依存更新は対象外とする。

## Decision Drivers

- 既存の結果不明・停止・重複などの分類を実画面まで保持する
- 内部診断をそのまま表示しない
- 全APIや生成バインディングを変更せず共通境界へ閉じる

## Considered Options

### 識別可能なJSON文字列をErrorFormatterから返す

Errorのmessageとして保持でき、Promise成功型を変えない。一方、識別子・形式・サイズの契約と解析失敗への対処が必要になる。

### 全APIを成功・失敗の結果型へ変更する

エラーも通常のJSONデータとして返せるが、全APIの戻り値と呼び出し元に変更が広がる。

### Wailsのランタイムを改変する

オブジェクトを保持できるが、生成物や依存forkの保守を増やすため採用しない。

## Decision

2026-10-20、利用者はJSON文字列で渡し、画面の共通処理で復元する方式を承認した。

ErrorFormatterは安全なcode/messageだけを`manga-update-manager:api-error:v1:`に続くJSON文字列へ変換する。全体はGo側で8192バイトまでとし、超過・変換失敗・分類できないGoエラーは固定のinternal_errorへ置き換える。元エラーや内部パスは含めない。

画面はError.messageが同じ識別子で始まり8192文字以内の場合だけJSONを解析する。配列・null・不正JSON・未知コード・空/非文字列/上限超過メッセージ・未知版は共通案内へ置き換える。生の文字列や通常のError.messageは表示しない。DB停止判定も同じ復元・検証を使う。既存の構造化オブジェクトも、既知コードと文言の同じ検査後に扱う。

識別子は認証・信頼性の証明ではなく、解析対象を限定する目印である。包装を通ることと、許可した情報だけを表示することを別々にテストする。

## Consequences

成功DTOとエラーコードを維持する。Go/JavaScript標準のJSONを使い、依存を増やさない。将来のWails更新時は包装との互換性を再確認する。Go側のバイト数制限を送出上限とし、画面の文字列長制限は解析前の上限とする。

## Validation

Goの外側JSON輸送、Error文字列包装、既知/未知/破損/上限超過の画面判定、結果不明・停止・確定後警告のUIを自動テストする。実Wails/WebView2で案内を再確認し、合成故障と実SQLite競合を区別して記録する。ADR-0013全体の検証完了とは別である。

2026-10-20、自動テストとWails v2.16.0の実WebView2で、結果不明・DB停止・画像だけの停止・確定後警告を確認した。故障注入のないproduction exeでもURL重複の専用案内を確認した。[検証記録と限界](../verification/cover-stop-ui.md)を参照する。この伝達方式のVerifiedであり、画像保存や全故障系の検証完了ではない。

## References

- [採用版Wails Callback](https://github.com/wailsapp/wails/blob/v2.16.0/v2/internal/frontend/runtime/desktop/calls.js)
- [採用版Dispatcher](https://github.com/wailsapp/wails/blob/v2.16.0/v2/internal/frontend/dispatcher/calls.go)
- [API設計](../work-api.md)
