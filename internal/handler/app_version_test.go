package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAppVersionIsPublic(t *testing.T) {
	mux := http.NewServeMux()
	(&Handler{}).Register(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/app/version", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("status=%d content-type=%q", w.Code, w.Header().Get("Content-Type"))
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"version": "1.2.3", "changelog": "Добавлен повтор «Ежеквартально»", "force_update": false}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
