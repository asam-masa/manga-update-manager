# Windowsのバックアップ非上書き公開Spike

Issue #400。2026-11-03、21:21:48〜21:30:07（日本時間）、約8分19秒で公開仕様と最小の合成試験を調査した。上限30分内で問いへの回答と残条件を整理し、以下を記録した。製品の保存・公開処理は未実装であり、これは方式候補の検証である。

## 結論と候補

ローカルNTFSの専用一時領域で、開いた元ファイルハンドルに対する`SetFileInformationByHandle(FileRenameInfo)`、`ReplaceIfExists=FALSE`による絶対パスへの変更が成功した。同名先あり・直前に同名を作った場合は衝突エラーとなり、先の内容と一時ファイルが残った。事前の存在確認だけによる非上書きではない。

候補は、祖先と保存先を保持して既存の再解析ポイントを拒否し、一時ファイルを`CREATE_NEW`・共有なしで作成して、同じハンドルで識別・読戻し・名前変更を行う方式である。これは推奨候補であり、製品で採用済みのファイル境界ではない。ドライブ再割当て、保持中の悪意ある属性変更、Close後の差し替えなどへの全面的な保証は確認していない。

`RootDirectory`に保存先ハンドル、`FileName`に相対名を指定する別経路も試したが、ローカルでは`ERROR_INVALID_PARAMETER`だった。成功した絶対パス方式と混同せず、非公開APIへの置換や設定変更は試していない。再現コードは、他の環境で成功した場合も出力内容を検証して観測をログに残す。相対指定の全Windowsでの可否は断定しない。

## 公開仕様

- [FILE_RENAME_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)は、ReplaceIfExistsがFALSEで同名先が存在するとエラーになると説明する。構造体の名前長はバイト単位。APIには[SetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle)を使用した。
- [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)のCREATE_NEWは既存名で失敗する。共有なしのファイルと、DELETE共有を付けないディレクトリーハンドルを試した。[再解析ポイント](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points-and-file-operations)は通常のファイル操作と挙動が異なるため、OPEN_REPARSE_POINTで開いて属性を確認する候補とした。
- [FILE_ID_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)のボリューム識別と128bitファイルIDを対応確認に使用した。これは現在の開いた対象の識別であり、パス文字列や専用接頭辞だけを削除の根拠にしない。
- [FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)とGoのFile.Syncを使った試験であり、名前変更と親ディレクトリーを含む電源断耐久性を実証したものではない。単一APIの成功から一連の処理の原子性を推測しない。

## 実行した試験

再現コード: [publication_windows_test.go](../../tools/spikes/backup-publication/publication_windows_test.go)。`go test ./tools/spikes/backup-publication -v -count=1`が制限外の通常権限環境で成功した。Windows 10.0.26300 amd64、Go 1.26.5、t.TempDirのNTFSで実測した。新しい依存ライブラリはなく、既存のgolang.org/x/sys/windows v0.49.0を使用した。

| 試験 | 結果と限界 |
| --- | --- |
| 同じハンドルによる公開 | 変更後も元のファイルIDとボリューム識別が一致し、ハンドルから取得した最終名も一致。Close後の再openでもIDと内容を照合。Closeから再openまでの無競合は保証しない |
| 既存先と直前競合 | いずれも既存名の衝突エラーを確認。先の内容と元一時ファイルが残る。スレッド競争の反復負荷試験ではない |
| 保持中の差し替え | 一時ファイルへの書込用再open、外部からの名前変更、保存先ディレクトリーの名前変更、CREATE_NEWの再作成を拒否 |
| 所有確認と片付け | ID一致・通常ファイル・リンク数1を確認し、同じハンドルでFileDispositionInfoを指定してClose後の不在を確認。ID不一致では保持。削除失敗をリトライで強行しない |
| 再解析ポイント | 専用一時領域のディレクトリーシンボリックリンクを拒否。作成権限がない環境ではこの試験だけSkipするが、制限外のローカル試験では実行できた |
| 権限不足の失敗 | 読取専用ハンドルでSyncと削除指定が失敗し、内容が残った。ディスク容量不足やハードウェア故障の再現ではない |
| 合成ZIP一連の流れ | WriteArchive→Sync→ValidateArchive→名前変更→Sync→Close→再open→ValidateArchiveとID照合が成功。DBはSQLiteではない合成バイト列で、意味的DB検証ではない |
| 取消・結果不明 | 切替前の取消ではAPIを呼ばず所有一時だけを片付ける。切替完了後の取消では完成物を残す。変更前/変更後に不明エラーを注入した模型では両状態をそれぞれ保持。実APIが結果を失う故障を起こした試験ではない |
| ディレクトリーハンドル相対指定 | ローカルでは引数エラー。元ファイルを保持し、相対先が作られていないことを確認 |

初回サンドボックスでは名前変更がアクセス拒否となり、シンボリックリンク作成も権限不足だった。同じ試験を承認済みの制限外実行へ切り替えたところ、絶対パス公開とリンク拒否の試験は成功した。サンドボックス内のアクセス拒否を、同名衝突による正常な拒否とは扱わない。ACLやOS設定は変更していない。

## 未検証と次の分割

実USB・NAS・NTFS以外、別ボリューム、ハードリンク競合、保持中の再解析情報変更、ドライブ割当て変更、電源断・プロセス強制終了・実容量不足・実Close失敗・長時間ブロックは未実測。通常保存先と製品API/UIは使っていない。試験内のデータはt.TempDirで管理し、実バックアップや秘密情報をGitへ保存しない。

次の実装前に、次の単位で具体契約を固定する。

1. Windows保存先と新規一時ファイルの境界。許可するパス/能力、祖先保持、再解析・リンク・ID確認、ハンドル所有と片付け条件を確定する。今回の絶対パス候補を無条件に一般化しない。
2. 書込・Sync・読戻し・非上書き公開・結果確認の順序と結果型。公開前の取消、公開後の保全、各API/Close失敗、結果不明を分け、失敗注入試験で確認する。単なる存在・ハッシュ一致だけで今回作成したと判断しない。
3. その後に通常操作停止・整合したDB/画像取得と接続し、保存ダイアログ・表示・実機確認へ進む。

問いに対して候補と阻害点を得た段階であり、製品全体のDoRや復元可能性を満たしたとは扱わない。[ADR-0022](../adr/0022-manual-backup-and-replacement-restore.md)のPendingを維持する。
