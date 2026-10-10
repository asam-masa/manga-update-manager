package main

type DisplaySettingsDTO struct {
	RegistrationFormVisible bool `json:"registrationFormVisible"`
}

func (a *App) LoadDisplaySettings() (DisplaySettingsDTO, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil {
		return DisplaySettingsDTO{}, displaySettingsReadError()
	}
	value, err := a.display.Load(a.ctx)
	if err != nil {
		return DisplaySettingsDTO{}, displaySettingsReadError()
	}
	return DisplaySettingsDTO{RegistrationFormVisible: value.RegistrationFormVisible}, nil
}

func displaySettingsReadError() error {
	return &APIError{Code: "settings_read_failed", Message: "表示設定を読み込めませんでした。登録フォームを表示して続行します。保存先を確認し、必要に応じてアプリを再起動してください。"}
}

func (a *App) SetRegistrationFormVisible(visible bool) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.display == nil || a.display.SetRegistrationFormVisible(a.ctx, visible) != nil {
		return &APIError{Code: "settings_save_failed", Message: "表示設定を保存できませんでした。再起動すると以前の状態に戻る場合があります。保存先の空き容量とアクセス権限を確認してください。"}
	}
	return nil
}
