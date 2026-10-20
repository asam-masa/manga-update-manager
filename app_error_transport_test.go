package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func decodeFormattedError(t *testing.T, err error) APIError {
	t.Helper()
	// Reproduce the dispatcher's outer JSON transport before Wails' new Error(string).
	outer, marshalErr := json.Marshal(formatAPIError(err))
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var wire string
	if err := json.Unmarshal(outer, &wire); err != nil {
		t.Fatal("formatter must return a string", err)
	}
	if !strings.HasPrefix(wire, apiErrorWirePrefix) || len(wire) > apiErrorWireLimit {
		t.Fatal("invalid error envelope")
	}
	var result APIError
	if err := json.Unmarshal([]byte(strings.TrimPrefix(wire, apiErrorWirePrefix)), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAPIErrorTransportKeepsSafeFields(t *testing.T) {
	want := &APIError{Code: "cover_outcome_unknown", Message: "結果を確認できません。\n再起動してください。"}
	got := decodeFormattedError(t, errors.Join(want, errors.New("private source path")))
	if got != *want {
		t.Fatal(got)
	}
	for _, err := range []error{errors.New("private source path"), &APIError{Code: "invalid_input", Message: strings.Repeat("x", apiErrorWireLimit)}} {
		got := decodeFormattedError(t, err)
		if got.Code != "internal_error" || strings.Contains(got.Message, "private") {
			t.Fatal(got)
		}
	}
}
