# Windows画像ファイル境界の事前検証

Issue #75のSpike。検証コードは[専用テスト](../../tools/spikes/cover-file-boundaries/boundaries_windows_test.go)に閉じ、アプリ本番から呼び出さない。利用者画像・DB・ACL・システム設定は変更しない。

## 問いと確認方法

既存の`x/sys/windows`で、上書き拒否、ハンドルの属性によるリンク拒否、保持中の付け替え拒否、確認済みハンドルへの削除要求を扱えるか確認する。各テストの`t.TempDir()`で合成ファイルだけを作成する。テスト後はGoのテスト機構がその一時領域を削除する。

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./tools/spikes/cover-file-boundaries -v
go vet ./tools/spikes/cover-file-boundaries
```

## 確認範囲

2026-10-12、Go 1.26.9・`CGO_ENABLED=0`のWindows環境で5テストがskipなしで成功した。全Goテストと`go vet ./...`も成功した。以下はこの合成ファイルと現在の環境での観測である。

- `CREATE_NEW`が既存ファイルを拒否し、既存内容を維持する。
- 新規作成した合成データの書き込み・同期・Close後の読取結果が一致する。
- 共有なしで保持したファイルのパスによる削除・名前変更・上書きが共有違反で拒否される。
- 属性・種類・リンク数を確認した同じハンドルへ`FileDispositionInfo`を渡し、Close後にそのファイルがなくなる。
- `FILE_FLAG_OPEN_REPARSE_POINT`で開いたsymlink、複数hard link、ディレクトリを通常ファイルとして受理しない。リンク先の合成データは維持する。
- 保持した`covers`ディレクトリ自身の名前変更・削除が拒否され、解放後には同じ対象の名前変更・削除が成功する。

削除テストは、テスト自身が作成した通常ファイルだけを対象にする。`FILE_DISPOSITION_INFO.DeleteFile`は1 byteのBOOLEANであり、4 byteのBOOLとは区別する。確認したハンドルを閉じてからパスで開き直して削除する方式は試さない。

## 限界と残る設計判断

この検証はOS操作の一部であり、本番の画像管理が安全だと証明するものではない。共有なしのハンドルは実ファイル操作を制限するが、「そのファイルをアプリが生成した」という履歴を証明しない。起動時整理の所有判定は別途必要である。

祖先ディレクトリの付け替え、junctionやその他のreparse tag、ネットワーク保存先、異なるファイルシステム、電源断耐久性、容量不足・同期・Close失敗、DBとの部分成功、削除権限不足は未検証。現在名で開くまでの安全な領域確認も本番実装の残る条件である。フォルダー一つの保持から祖先全体の安全性を推論しない。

今回のテストはファイル名や属性だけを削除の許可根拠にする設計を採用しない。結果を踏まえて[画像保存契約](../cover-persistence.md)の未決定事項を具体化する。Issue #42とADR-0013全体は未完了・Pendingのまま。

## 一次資料

- [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)
- [SetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle)
- [FILE_DISPOSITION_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_disposition_info)
