package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a := newApp()
	a.dataDir = t.TempDir()
	a.projectDir = filepath.Join(t.TempDir(), "olcrtc")
	if err := a.saveCredentials("admin", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	return a
}
func request(a *App, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, r)
	return w
}
func loginCookie(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	w := request(a, "POST", "/api/login", `{"username":"admin","password":"correct-horse-battery"}`, nil)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func TestAuthentication(t *testing.T) {
	a := testApp(t)
	if w := request(a, "GET", "/api/settings", "", nil); w.Code != 401 {
		t.Fatalf("want 401 got %d", w.Code)
	}
	if w := request(a, "POST", "/api/login", `{"username":"admin","password":"wrong"}`, nil); w.Code != 401 {
		t.Fatalf("bad password: %d", w.Code)
	}
	c := loginCookie(t, a)
	if w := request(a, "GET", "/api/settings", "", c); w.Code != 200 {
		t.Fatalf("authenticated: %d", w.Code)
	}
}
func TestSaveSettingsWritesEnv(t *testing.T) {
	a := testApp(t)
	c := loginCookie(t, a)
	body := `{"provider":"cloudflare","transport":"websocket","domain":"rtc.example.com","listenAddress":"0.0.0.0","listenPort":8443,"upstream":"","autoStart":true}`
	w := request(a, "PUT", "/api/settings", body, c)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	b, e := os.ReadFile(filepath.Join(a.projectDir, ".env"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "OLCRTC_TRANSPORT=websocket") {
		t.Fatalf("unexpected env: %s", b)
	}
	var s Settings
	if e = json.Unmarshal(w.Body.Bytes(), &s); e != nil || s.ListenPort != 8443 {
		t.Fatalf("response: %v %#v", e, s)
	}
}
func TestRejectsUnsafeSettings(t *testing.T) {
	a := testApp(t)
	c := loginCookie(t, a)
	body := `{"provider":"x\nBAD=value","transport":"udp","listenAddress":"0.0.0.0","listenPort":1,"autoStart":false}`
	if w := request(a, "PUT", "/api/settings", body, c); w.Code != 400 {
		t.Fatalf("want 400 got %d", w.Code)
	}
}
