# SQLiteコピーと出力ハンドルの限定Spike（#454）

## 結果

2026-11-06、採用中のmodernc.org/sqlite v1.61.1・libc v1.79.0とWindowsの合成一時領域で、VACUUM INTOと出力ファイル保持の共有条件を比較した。読取/書込ハンドルを読取/書込共有で保持する条件ではコピーとID維持を確認したが、削除権限を持つ保持や共有なしの保持はコピーに失敗した。コピー後の同一オブジェクトのDELETE権限付きDuplicateHandleもアクセス拒否で、連続保持したままの片付け方式は未成立である。

調査上限は30分。初期結果を得たところで代替探索を打ち切った。[再現コード](../../tools/spikes/backup-db-handle/handle_windows_test.go)はt.TempDir内の合成DBと、製品の作業フォルダー境界だけを使う。製品StoreやApplicationの取得経路には接続しない。ACL変更・権限設定変更・通常データへの操作・非公開API・新規依存はない。

## 仕様とコード上の根拠

- [SQLite VACUUM](https://www.sqlite.org/lang_vacuum.html)は、INTOの出力先が未作成または空ファイルである必要、出力DBの一貫性、途中中断で不完全なコピーが残り得ることを説明する。外部画像との整合性やファイルの所有確認までは保証しない。
- 採用版の[Windows VFS](https://gitlab.com/cznic/sqlite/-/blob/v1.61.1/lib/sqlite_windows.go)のwinOpenは、通常の共有モードをFILE_SHARE_READ | FILE_SHARE_WRITEとする。DELETE権限付きの既存ハンドルとの共有互換性は実測で確認した。
- [ReOpenFile](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-reopenfile)も既存のアクセス/共有モードと衝突する要求はできないと規定する。名称だけから安全な権限変更ができると判断せず、今回は試していない。
- [DuplicateHandle](https://learn.microsoft.com/en-us/windows/win32/api/handleapi/nf-handleapi-duplicatehandle)は同じオブジェクトを指すハンドルを作り、要求するアクセス権を指定できる。この候補を一度だけ試したが、DELETE権限の取得は手元で拒否された。追加の権限・別APIで通そうとはしない。

## 実測した範囲

`go test ./tools/spikes/backup-db-handle -v -count=1`を通常権限で実行。3条件の試験は成功したが、これは下表の成否を観測できたという意味で、片付け可能なバックアップ方式の完成ではない。

| 作成時のアクセスと共有 | VACUUM INTO | 保持とコピー後の観測 |
| --- | --- | --- |
| READ/WRITE/DELETE、READ/WRITE共有 | SQLiteエラー14で拒否 | 作成前後のID維持と、保持中の改名拒否を確認。読めるDBコピーの完成は確認していない |
| READ/WRITE、READ/WRITE共有 | 成功 | 同じID、作品ID/タイトル、quick_check=ok、保持中の改名拒否を確認 |
| READ/WRITE/DELETE、共有なし | SQLiteエラー14で拒否 | 作成前後のID維持と、保持中の改名拒否を確認。読めるDBコピーの完成は確認していない |

成功条件でSQLiteの検証用接続を閉じた後、元の保持ハンドルを閉じずにDELETE | FILE_READ_ATTRIBUTESを要求するDuplicateHandleを呼んだ。Access is deniedで、削除指定は実行していない。所有ハンドルと作業フォルダーはCloseで解放し、残置物の掃除はt.TempDirの試験用処理だけに任せた。これを製品の残置削除権限として扱わない。

APIが使えない環境では理由付きSKIPとなる。DuplicateHandle・削除指定の候補が失敗しても試験は観測結果をログへ残して終了するため、パッケージのPASSだけでその能力を確認済みとしない。ACLや権限設定の変更、所有ハンドルを閉じてパス指定で取り直す処理、パス削除への切替は行わない。内容確認用のSQLite接続は、元の保持ハンドルを残したまま同じ出力名を開いている。

## 後続判断と限界

VACUUM INTOの採用自体は変更しないが、現在の一時ZIPと同じDELETE権限付き保持をDBコピーへそのまま流用することはできない。DBコピー取得と片付けを製品へ接続する前に、次の選択肢を比較する。

- コピー中は改名を抑える保持を続け、完了後に解放・再取得と厳密な再確認を行う案。保持の連続性を失う区間と、確認不能な場合の保全を新たに設計する必要があり、今回の試験で安全性は確認していない。
- コピー方式や出力の扱いを別のSpikeで調べ直す案。追加調査が必要で、現行方式より良いと確認できたものではない。

同時更新中の製品DB/画像の整合取得、停止・取消・容量不足、外部書込、実USB、電源断、全OS/全共有条件は未検証。READ/WRITE共有は他者の書込を禁止しない。ID一致とquick_checkを外部変更・真正性の保証にしない。判断の正本は[初回設計](../backup-restore.md)と[ADR-0022](../adr/0022-manual-backup-and-replacement-restore.md)で、製品採用の追加判断と故障検証が残るためPendingを維持する。
