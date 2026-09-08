package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
	body := `{"provider":"jitsi","transport":"datachannel","providerAddress":"","room":"rtc-room","listenAddress":"127.0.0.1","listenPort":1080,"destination":"example.com:443","autoStart":true}`
	w := request(a, "PUT", "/api/settings", body, c)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	b, e := os.ReadFile(filepath.Join(a.projectDir, ".env"))
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(b), "OLCRTC_TRANSPORT=datachannel") {
		t.Fatalf("unexpected env: %s", b)
	}
	var s Settings
	if e = json.Unmarshal(w.Body.Bytes(), &s); e != nil || s.ListenPort != 1080 {
		t.Fatalf("response: %v %#v", e, s)
	}
}
func TestRejectsUnsafeSettings(t *testing.T) {
	a := testApp(t)
	c := loginCookie(t, a)
	body := `{"provider":"cloudflare","transport":"udp","listenAddress":"127.0.0.1","listenPort":1,"autoStart":false}`
	if w := request(a, "PUT", "/api/settings", body, c); w.Code != 400 {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestSupportedOLCRTCProvidersAndTransports(t *testing.T) {
	for _, provider := range []string{"jitsi", "telemost", "wbstream", "none"} {
		for _, transport := range []string{"datachannel", "vp8channel", "seichannel", "videochannel"} {
			s := defaultSettings()
			s.Provider, s.Transport = provider, transport
			if err := validateSettings(s); err != nil {
				t.Errorf("%s/%s rejected: %v", provider, transport, err)
			}
		}
	}
	s := defaultSettings()
	s.Provider = "cloudflare"
	if err := validateSettings(s); err == nil {
		t.Error("unsupported provider accepted")
	}
}

func TestComposeDetection(t *testing.T) {
	a := testApp(t)
	if a.hasComposeFile() {
		t.Fatal("empty project unexpectedly has a compose file")
	}
	if err := os.MkdirAll(a.projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.projectDir, "compose.yaml"), []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !a.hasComposeFile() {
		t.Fatal("compose.yaml was not detected")
	}
}

func TestInstallUpdateIntoDirectoryContainingGeneratedEnv(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "upstream")
	if err := os.Mkdir(repository, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repository}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "master")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repository, "README.md"), []byte("OLC RTC\n"), 0644); err != nil {
		t.Fatal(err)
	}
	git("add", "README.md")
	git("commit", "-m", "initial")

	a := testApp(t)
	a.repository = repository
	if err := a.writeSettings(defaultSettings()); err != nil {
		t.Fatal(err)
	}
	w := request(a, http.MethodPost, "/api/update/install", "", loginCookie(t, a))
	if w.Code != http.StatusOK {
		t.Fatalf("install: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.projectDir, "README.md")); err != nil {
		t.Fatalf("checked out repository missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.projectDir, ".env")); err != nil {
		t.Fatalf("generated settings missing: %v", err)
	}
}

func TestInitialPageHidesPanelAndServesAtRoot(t *testing.T) {
	a := testApp(t)
	root := request(a, http.MethodGet, "/", "", nil)
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), `id="panel" hidden`) {
		t.Fatalf("root page: %d %s", root.Code, root.Body.String())
	}
	css := request(a, http.MethodGet, "/style.css", "", nil)
	if css.Code != http.StatusOK || !strings.Contains(css.Body.String(), `[hidden]{display:none!important}`) {
		t.Fatalf("hidden rule missing: %d %s", css.Code, css.Body.String())
	}
	legacy := request(a, http.MethodGet, "/web/", "", nil)
	if legacy.Code != http.StatusPermanentRedirect || legacy.Header().Get("Location") != "/" {
		t.Fatalf("legacy URL: %d %q", legacy.Code, legacy.Header().Get("Location"))
	}
}
