# 画像選択中の親ウィンドウ前面化とWebView2終了

2026-10-20の追加判断: #176の特殊な自動前面化による終了は未修正の既知制約として継続し、その修正自体をADR-0013の検証完了の必須条件にしない。通常操作で再現した場合は必須の修正対象へ戻し、完了判定を再評価する。以下の未修正・未確認という観測は変えない。[現在の完了判定](../adr/0013-define-cover-operation-boundaries.md#現在の完了判定issue-178)を参照する。

Issue #171のSpike結果。2026-10-19、`main`の`25c2c7470240fd5418385920c4a58f41e1b6ac08`で再確認した。結論は、画像選択中の無効な親ウィンドウに対する自動前面化で終了を再現したことであり、不具合の修正ではない。残る問題は[Issue #176](https://github.com/asam-masa/manga-update-manager/issues/176)で追跡する。

## 環境と分離

- Windows、Go 1.26.9、CGO_ENABLED=0、Wails v2.16.0、go-webview2 v1.0.22、WebView2 Runtime 155.0.4283.62。
- 開発モードは`dev -m -nosyncgomod -nogorebuild -noreload -nocolour`で起動した。production exeはPR #175の画像再取得修正を含むビルド済み成果物を使用した。今回、本番コード・依存は変更していない。
- 子プロセスのAPPDATA/TEMP/TMPを`D:\appdata_local_tmp\mum-dev-cover-9355e9d1b8f84b0b8188dbc7e6974df5`内へ分離した。合成3作品と合成PNGだけを使い、利用者のDB・画像には触れていない。
- UI Automationの対象を確認用PIDに限定し、選択画面の有無と親の`IsWindowEnabled`を観測した。別アプリの画面を撮影せず、OS/ACL/Explorerの設定も変更していない。

## 条件ごとの結果

| 条件 | 観測 | 限界 |
| --- | --- | --- |
| 開発モード、ダイアログなし | 自動前面化5回、最小化・復元3回で終了しなかった | 全ての起動タイミングではない |
| 開発モード、画像選択中の手動操作 | 利用者がAlt+Tabで別アプリへ移動して戻り、キャンセルしても終了しなかった | 1回の確認手順。通常操作全体の保証ではない |
| 開発モード、画像選択中の親への自動前面化 | 親は無効。最初の`SetForegroundWindow(parent)`がtrueを返した直後に終了。`Focus → errorCallback`とファイル選択のスタックを記録 | 保存中の中断ではない |
| production exe、ダイアログなし | 自動前面化5回で終了せず、画像選択とキャンセルも成功 | 手動Alt+Tabはこのexeでは未確認 |
| production exe、画像選択中の親への自動前面化 | 開発モードと同じ条件・スタックで終了 | 開発モード限定ではない。各モード1回の再現 |
| production exe、起動直後 | 起動83ms後に有効な同名の所有ウィンドウを検出して表示・前面化を要求。5回中2回true、3回false。プロセスは継続した | OSが全要求を受け付けたわけではなく、全初期化段階の検証ではない |
| 起動直後試験の後 | 非表示起動された所有ウィンドウを表示し直し、選択・キャンセル・通常終了が成功 | 上記エラーを修正した結果ではない |

選択ダイアログ自体の自動前面化も試したが、`SetForegroundWindow`はfalseで、前面化できたとは確認していない。これを成功した比較条件へ含めない。

## 終了するコード経路と未確定の部分

採用ソースでは、[WailsのonFocus](https://github.com/wailsapp/wails/blob/v2.16.0/v2/internal/frontend/desktop/windows/frontend.go#L1042)が無条件に`chromium.Focus()`を呼ぶ。[go-webview2のFocus](https://github.com/wailsapp/go-webview2/blob/v1.0.22/pkg/edge/chromium.go#L572)は`MoveFocus(PROGRAMMATIC)`の失敗を`errorCallback`へ渡し、[errorCallback](https://github.com/wailsapp/go-webview2/blob/v1.0.22/pkg/edge/chromium.go#L150)は`os.Exit(1)`する。再現ログの先頭と一致する。親の無効状態、前面化要求、直後のこの経路への到達までを観測した。

Microsoftの[SetFocus仕様](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setfocus)には、無効なウィンドウではERROR_INVALID_PARAMETERとなる条件がある。ただし、今回呼び出したのは親へのSetForegroundWindowと、Wails内の[WebView2 MoveFocus](https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2controller#movefocus)である。WebView2内部の失敗理由や数値HRESULTを直接取得していないため、SetFocusの条件をそのまま今回のエラー原因と断定しない。

アプリの本番コードには、親ウィンドウを強制前面化する呼び出しを確認していない。今回の再現入力は検証補助が加えた操作である。ただし、通常操作で同じ経路へ絶対に入らないとは確認していない。

## 異常終了と通常終了後のデータ

確認用SQLiteを`mode=ro`で開き、各異常終了後と最後の通常終了後に次を確認した。

- `PRAGMA quick_check`は`ok`、作品は3件。ID順のID・URL・タイトル・画像参照・登録/更新/最終アクセス日時をまとめたSHA-256は、再現前と同じ`5BC744E6CDB568AC84C03D9390F159BE7775DFAEB0BCF864277FC8D485A66CF9`だった。
- 合成元PNGのSHA-256は`C97C8C8535E7B1C7DA5962C8C3C7ED19ADE26E9ABBA2C1E9665A88F9F1DFFF42`で不変だった。
- 起動中は同じ保存先のロック取得が拒否され、終了後には再取得・解放できた。ロックファイルは削除していない。

選択をキャンセルするか、選択中に終了させただけの検証である。画像保存中・DB更新中の異常終了、全テーブル・全管理ファイルの不変性、容量不足時の回復まで保証しない。

## 記録の扱いと後続

今回の検証では、選択中の無効な親を前面化しない通常操作との比較が切り分けに有効だった。検証補助では親の状態とモーダル画面を区別する。これは本番での対策を実装したものではない。

本番対策の要否と方式は#176で判断する。依存更新・fork・フォーカスエラーの無視は採用していない。通常利用での追加再現、別のOS/Runtime、起動と終了の全競合は未確認である。ADR-0013の停止・長時間処理などの検証も残るため、Pendingを維持する。

異常終了で残った今回のGo/Wails/Vite補助プロセスは親子関係と名前を確認して停止した。最後のexeは通常終了した。調査スクリプト、合成DB・画像、元のスタック全文とログはGitへ追加しない。
