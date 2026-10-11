# ADR-0015: Goの最低版と検証版を分ける

- Status: Accepted
- Validation Status: Verified
- Date: 2026-10-12
- Related: [#67](https://github.com/asam-masa/manga-update-manager/issues/67)、[PR #62](https://github.com/asam-masa/manga-update-manager/pull/62)

## Context

x/image v0.47.0はGo 1.26.0を要求する。Go 1.25系は1.27公開後のサポート対象外である。最低版を上げるだけでは、古い修正版で検証・ビルドし続ける問題が残る。対象はGoと関連依存の更新であり、Go 1.27移行、DB仕様変更、システムGoの置換は含めない。

## Decision Drivers

- 依存が必要とするGo版を満たす
- サポート中の系列の修正を取り込む
- 不要な移行を混ぜず、検証版を特定できる

## Considered Options

### Go 1.25を維持する

変更量は少ないが、x/image更新条件を満たさずサポート対象外の系列を維持する。

### 最低版1.26、検証版は1.26系の最新修正版

必要版を満たし、修正を取り込める。一方、検証版は時間とともに変わるため、結果には実際の版を残す必要がある。

### Go 1.27へ移行する

最新系列へ進めるが、依存の必要条件を超える移行と検証範囲を今回に追加する。

## Decision

2026-10-12のユーザー承認により、最低Go版を1.26.0、テスト・ビルドは1.26系の最新修正版とする。CIはsetup-goの`1.26.x`と`check-latest: true`で選択し、`GOTOOLCHAIN=local`とCGO無効を維持する。Go 1.27は別途判断する。

## Consequences

Go 1.25での開発は対象外になる。最低版は利用可能な下限であり、その版での実行を今回検証した意味ではない。CIログとPRへ実際のGo版・対象SHAを記録する。将来のGo系列更新は、依存更新とは区別して判断する。利用者の保存データ形式は変更しない。

## Validation

2026-10-12、Go 1.26.9・CGO_ENABLED=0・Wails 2.16.0・Vite 7.3.7で、全Goテスト・vet、画面44テスト、設定4テスト、フロントエンドとWindowsのビルドが成功した。利用者データを使わない実WebView2で、起動・終了6項目と画像配信の開発・本番各9項目が成功した。開発モードの初回は検証スクリプトが新旧CLIを同時に拾って起動に失敗した。検証プロセス内だけのPATHで2.16.0へ限定して再実行した。システム環境は変更していない。

通常・独立レビューは指摘なし。画面全体の手動操作と最低版1.26.0での実行は未確認。結果と固定版はPRへ記録した。

2026-10-12、[PR CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38152819436)のhead `f1cbbd179649358bf66c0875b453006cae47179d`と、マージ後の[main CI](https://github.com/asam-masa/manga-update-manager/actions/runs/38153065525)のSHA `d18616443533ed69d367bf20b521f8b709d2e0e6`で全検証が成功した。両CIのSet up GoログでGo 1.26.9を確認した。今回の設定・依存版について、最新修正版の選択とテスト・ビルド、ローカルの実WebView2検証を完了したためVerifiedとする。最低版単独、画面全体の手動操作、将来の修正版やGo 1.27の成立を保証しない。

## References

- [Goのサポート方針とリリース履歴](https://go.dev/doc/devel/release)
- [Go 1.26の変更](https://go.dev/doc/go1.26)
- [x/image v0.47.0のgo.mod](https://proxy.golang.org/golang.org/x/image/@v/v0.47.0.mod)
- [setup-go](https://github.com/actions/setup-go/tree/v7.0.0)
