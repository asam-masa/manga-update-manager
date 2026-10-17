# 新規画像ファイルの保存と再取得照合

Issue #111。Windows専用`platform.WriteNewCover(ctx, directory, id, data)`は未接続の保存境界である。呼び出し側が管理IDの予約と`thumbnail.Generate`を終えてから使う。ID生成・SQLite操作・作品参照変更・元画像選択・PNG形式検証は行わない。入力は生成済みPNGを前提とする非空バイト列であり、この関数自身はPNGのデコーダーではない。

## 処理と結果

- IDを小文字16進32文字のまま検証し、空データを拒否する。任意のファイル名や相対パスは受け取らない。
- `PrepareCoverDirectory`で祖先とcoversを保持し、`covers/<id>.png`を読取・書込、共有なし、CREATE_NEWで作る。衝突時は既存ファイル・リンク・ディレクトリを上書きしない。
- 短い書き込み、書き込み・同期のエラーを確認する。書込後の同じハンドルで通常ファイル・リンク数1、128bitファイルIDとvolume serial、サイズ、SHA-256を取得し、入力サイズ・ハッシュと照合する。
- Close成功後に共有なし・reparse pointをたどらない読取ハンドルで開き直し、全情報の一致を確認して再度Closeする。全ファイル操作を終えてからフォルダーの保持を解放する。
- 成功時だけ`CoverFileInfo`を返す。uint64のvolume serial、API順序を保つ16 byte FileID、正のint64サイズ、32 byte SHA-256を持つ。storageの型には依存せず、後続Applicationで変換する。取得値は削除許可ではない。

失敗・キャンセル・ファイルまたはフォルダーのCloseエラーでは確認情報のゼロ値とエラーを返す。ファイルとフォルダーのCloseを試し、エラーを結合する。明示Close後は同じOSハンドルを再Closeしない。キャンセルは処理の境目で確認し、OS API実行中の即時中断は保証しない。

作成後の失敗では空または部分ファイルが残り得るが、自動削除しない。予約だけの記録や今回の名前だけで所有・削除可能と扱わない。識別情報を取れなければ成功にせず、旧作品参照は後続の呼び出し側が変更しない。元画像・既存ファイル・DBへ触れない。

上位のApplicationでは、解放を確認できないClose失敗時に画像機能を停止し、再起動を案内する方針を#113で合意した。[Close失敗と通常終了](../cover-persistence.md#close失敗と通常終了)を参照する。#121で、画像ファイル・保持フォルダーのClose失敗を`errors.Is(err, platform.ErrCoverRelease)`で判別できるようにした。元のClose原因と併発した処理エラーも保持する。この関数自身は停止状態やUIを管理せず、Applicationへの停止接続は後続で行う。

`ErrCoverRelease`は解放を確認できないことを示すだけで、実際のリークや復旧可能性を証明しない。成功・通常の保存失敗・キャンセルだけでは付けない。キャンセルとClose失敗が併発した場合は両方を判別できるため、キャンセルだから解放失敗を無視してよいとは扱わない。元のエラーに内部パスなどが含まれ得るので、そのまま利用者向け文言へ出さない。

#123でApplicationの`CoverOperations.ObserveError`がこの印から受付停止を行う部品を追加した。実際の`WriteNewCover`呼び出しからの通知と画面案内はまだ未接続である。[通知契約](./cover-admission.md#解放失敗の通知)を参照する。

## 検証と限界

2026-10-17、Windows・Go 1.26.9・CGO無効で合成PNGと一時領域だけを使う。

- Generate出力の保存、サイズ・SHA-256・FILE_ID_INFOのレイアウト、再オープン時の全情報一致、同じIDの上書き拒否
- リンク・ディレクトリ・共有取得失敗・不正ID・空入力・事前キャンセルを拒否し、合成元画像を保持
- 作成直後キャンセルでは確認情報を返さず、空ファイルを残し、ハンドルを解放
- 短い書き込み・書込エラー・同期エラーを消費側の小さなinterfaceで合成注入。実OSの容量不足・同期故障・Close故障の実証ではない
- 同一内容の別ファイルは識別情報が異なること、同サイズの内容変更はハッシュが異なること、複数hard link・閉じたハンドルでの取得は拒否すること

```powershell
$env:GOTOOLCHAIN='go1.26.9'
$env:CGO_ENABLED='0'
go test ./internal/platform -run 'TestWriteNewCover|TestCoverWrite|TestCaptureCover' -v
go test ./...
go vet ./...
go build ./...
```

実ファイルの付け替えをCloseと再取得の間へ注入するテスト、途中終了・次回整理、全FS・ネットワーク・悪意ある属性変更は未検証。PNG生成済みの前提を公開APIの検証省略へ転用しない。フォルダー保持はこの関数のファイル操作中だけであり、DB確定・後処理までの追加保持とアプリ内排他・終了待ちは後続で接続する。画像機能全体とADR-0013はPendingを維持する。

一次資料: [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)、[FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)、[FILE_ID_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_id_info)。
