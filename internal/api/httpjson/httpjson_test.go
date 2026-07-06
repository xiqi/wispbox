package httpjson

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"email":"a@example.com"}{"extra":true}`))
	rec := httptest.NewRecorder()

	var body struct {
		Email string `json:"email"`
	}
	err := Decode(rec, req, &body, 4096)
	if err == nil {
		t.Fatal("Decode accepted two JSON values in one request body")
	}
	if !strings.Contains(err.Error(), "exactly one JSON value") {
		t.Fatalf("Decode error = %v, want trailing JSON message", err)
	}
}
