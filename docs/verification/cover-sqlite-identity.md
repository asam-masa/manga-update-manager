# SQLite保存した識別情報とWindowsファイルの照合

Issue #85のSpike。[専用テスト](../../tools/spikes/cover-file-boundaries/sqlite_identity_windows_test.go)で、一時SQLiteに保存した確認情報を読み戻し、既存のWindows照合試作へ渡す。本番マイグレーション、作品参照、削除処理は変更しない。

## 候補と確認方法

volume serialは小文字16進16文字、128bitファイルIDはAPIが返した16 byteを順序を変えず小文字16進32文字にする。volume serialをSQLiteの符号付きINTEGERへ変換しない。サイズは正のINTEGER、SHA-256は32 byteのBLOBとする。試作の`snapshots`は確認情報の保存表現だけを検証し、管理記録の予約・保存確認済み状態を置き換えない。

STRICT、NOT NULL、長さ、16進文字、埋め込みNUL拒否を候補テーブルへ定義する。読戻し側でもデコードエラー、長さ、正のサイズを確認し、失敗時の部分値を一致判定へ渡さない。

- 実ファイルの確認情報を保存し、DBをClose・再接続して全項目を比較する。
- 読戻した情報で同一ファイルは一致、同内容の別ファイルと同サイズの内容変更は不一致となることを確認する。
- volume serial全bitが1、ファイルID上位bitを含む合成値で切り捨てを確認する。
- DBが閉じられた場合と記録不存在では一致と扱わない。削除要求はなく、合成ファイルを維持する。

## 実行

2026-10-12、Go 1.26.9・`CGO_ENABLED=0`のWindows環境で対象10テスト（追加2テスト）がskipなしで成功した。上記の結果は合成ファイルと一時SQLiteに限定した観測である。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/cover-file-boundaries -v
go test ./...
go vet ./...
```

実行結果とレビュー対象の版はPRへ記録する。アプリ本番を変更しないため、GUI確認は対象外とする。

## 限界と後続

候補表現の往復と照合は、本番の削除許可を証明しない。生成管理記録の状態、全作品の未参照確認、祖先保護、競合排他、削除APIへの統合は未検証である。ReFS実機や再起動での識別情報保持も未検証。[画像保存契約](../cover-persistence.md)とADR-0013全体は未完了・Pendingを維持する。

[前回SQLite候補](./managed-cover-schema.md)は従来API用の表現を扱った履歴として残す。今回の保存表現を管理記録の状態制約と合わせることは後続で検証する。本番採用前に仕様を記録し、試作コードをそのまま本番へコピーしない。
