# Windows画像ファイルの識別・内容照合

Issue #83のSpike。既存のWindows境界試作へ[専用テスト](../../tools/spikes/cover-file-boundaries/identity_windows_test.go)を追加した。本番から呼び出さず、合成ファイルと`t.TempDir()`だけを使う。

## 問いと結果

2026-10-12、128bitのファイルID、volume serial、サイズ、SHA-256を同じ開いたハンドルから取得・照合する方針を承認した。取得できなければ削除せず保持・警告する方針である。本番の警告処理は今回実装しない。

`GetFileInformationByHandleEx`の`FileIdInfo`で64bitのvolume serialと128bitのファイルIDを取得する。試作の構造体は24 byte、IDのオフセットは8 byteであり、テストでも確認する。従来の64bitファイルIDはReFSで一意性が保証されないため、そのまま本番方式へ採用しない。

共有なしで開いたハンドルの種類・属性・リンク数を既存の試作で確認し、同じハンドルからサイズと全内容のハッシュを取得する。読取バイト数とサイズも照合する。比較は全項目が一致し、取得エラーがない場合だけ真になる。

現在のWindows環境で、次の結果を確認した。

- 開き直した同一ファイルの識別情報・サイズ・ハッシュが一致する。
- 記録サイズだけを変えると不一致になる。
- 同じ内容・サイズの別ファイルは識別情報が異なり、不一致になる。
- 同じファイルの内容だけを同サイズで変更すると識別情報は同じ、ハッシュは異なり、不一致になる。
- 書込専用ハンドルによる読取失敗と、閉じたハンドルによる取得失敗では、ゼロ値の記録とも一致しない。合成ファイルは維持される。

## 実行

Go 1.26.9、`CGO_ENABLED=0`で対象パッケージの8テスト（追加3テスト、失敗系2サブテスト）がskipなしで成功した。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/cover-file-boundaries -v
go test ./...
go vet ./...
```

全体の検証結果とレビューはPRへ記録する。新しい照合テストには削除要求を実装していない。既存の削除API試作とは独立した確認である。

## 限界と後続作業

ReFS実機、他ファイルシステム、ネットワーク保存先、再起動・ファイルID再利用、祖先ディレクトリの付け替え、DB参照との競合は未検証である。識別情報とハッシュの一致だけで生成履歴を証明しない。SQLite管理記録・全作品の未参照確認・安全な領域確認を併用する必要がある。

前回の[SQLite候補](./managed-cover-schema.md)は従来API用の8文字volume serialと16文字file IDを検証した履歴であり、本番未採用である。今回の方式なら16文字volume serial・32文字file IDなどの保存表現を改めて検証する必要がある。今回その候補テーブルは変更しない。

本番採用前に[画像保存契約](../cover-persistence.md)を具体化する。Issue #42は未完了、ADR-0013はPendingを維持する。

## 一次資料

- [FILE_ID_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)
- [従来識別情報とReFSの制約](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/ns-fileapi-by_handle_file_information)
