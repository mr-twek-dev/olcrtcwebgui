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
	a.systemdDir = filepath.Join(t.TempDir(), "systemd")
	a.memoryInfoPath = filepath.Join(t.TempDir(), "meminfo")
	a.swapFile = filepath.Join(t.TempDir(), "swapfile")
	if err := os.WriteFile(a.memoryInfoPath, []byte("MemTotal:       8388608 kB\nSwapTotal:            0 kB\n"), 0600); err != nil {
		t.Fatal(err)
	}
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
func validSettings() Settings {
	s := defaultSettings()
	s.RoomID = "https://meet.example.org/rtc-room"
	s.CryptoKey = strings.Repeat("ab", 32)
	return s
}

func TestSaveSettingsWritesYAML(t *testing.T) {
	a := testApp(t)
	c := loginCookie(t, a)
	s := validSettings()
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	w := request(a, "PUT", "/api/settings", string(body), c)
	if w.Code != 200 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	b, e := os.ReadFile(filepath.Join(a.projectDir, "olcrtc.yaml"))
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"mode: srv", "provider: jitsi", "transport: datachannel", `dns: "8.8.8.8:53"`, `key: "` + s.CryptoKey + `"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("YAML does not contain %q:\n%s", want, b)
		}
	}
	var response Settings
	if e = json.Unmarshal(w.Body.Bytes(), &response); e != nil || response.RoomID != s.RoomID {
		t.Fatalf("response: %v %#v", e, response)
	}
}
func TestRejectsUnsafeSettings(t *testing.T) {
	a := testApp(t)
	c := loginCookie(t, a)
	s := validSettings()
	s.Provider = "cloudflare"
	body, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(a, "PUT", "/api/settings", string(body), c); w.Code != 400 {
		t.Fatalf("want 400 got %d", w.Code)
	}
}

func TestSupportedOLCRTCProvidersAndTransports(t *testing.T) {
	for _, pair := range [][2]string{{"jitsi", "datachannel"}, {"jitsi", "vp8channel"}, {"telemost", "vp8channel"}, {"telemost", "videochannel"}, {"wbstream", "vp8channel"}, {"wbstream", "seichannel"}} {
		s := validSettings()
		s.Provider, s.Transport = pair[0], pair[1]
		if err := validateSettings(s); err != nil {
			t.Errorf("%s/%s rejected: %v", pair[0], pair[1], err)
		}
	}
	s := validSettings()
	s.Provider = "cloudflare"
	if err := validateSettings(s); err == nil {
		t.Error("unsupported provider accepted")
	}
	s = validSettings()
	s.Provider, s.Transport = "telemost", "seichannel"
	if err := validateSettings(s); err == nil {
		t.Error("unsupported telemost/seichannel accepted")
	}
	s = validSettings()
	s.Provider, s.Transport = "wbstream", "datachannel"
	if err := validateSettings(s); err == nil {
		t.Error("wbstream/datachannel without account token accepted")
	}
}

func TestClientAndTransportYAML(t *testing.T) {
	s := validSettings()
	s.Mode = "cnc"
	s.Transport = "seichannel"
	s.SOCKS.User, s.SOCKS.Pass = "proxy-user", "proxy-pass"
	if err := validateSettings(s); err != nil {
		t.Fatal(err)
	}
	yaml := settingsYAML(s)
	for _, want := range []string{"socks:\n", `host: "127.0.0.1"`, "port: 8808", "sei:\n", "fragment_size: 900"} {
		if !strings.Contains(yaml, want) {
			t.Fatalf("YAML does not contain %q:\n%s", want, yaml)
		}
	}
}

func TestInstallUpdateBuildsAndInstallsService(t *testing.T) {
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
	var calls []string
	a.envRunner = func(environment []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if name == "mage" {
			if err := os.MkdirAll(filepath.Join(a.projectDir, "build"), 0755); err != nil {
				return nil, err
			}
			return nil, os.WriteFile(a.binaryFile(), []byte("binary"), 0755)
		}
		return runWithEnv(environment, name, args...)
	}
	a.runner = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if name == "systemctl" || name == "systemd-analyze" {
			return nil, nil
		}
		return run(name, args...)
	}
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
	if _, err := os.Stat(filepath.Join(a.projectDir, "olcrtc.yaml")); err != nil {
		t.Fatalf("generated settings missing: %v", err)
	}
	unit, err := os.ReadFile(filepath.Join(a.systemdDir, "olcrtc.service"))
	if err != nil {
		t.Fatalf("systemd unit missing: %v", err)
	}
	for _, want := range []string{"WorkingDirectory=" + filepath.Clean(a.projectDir), "ExecStart=" + filepath.Clean(a.binaryFile()) + " " + filepath.Clean(a.configFile()), "Restart=on-failure"} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("unit does not contain %q:\n%s", want, unit)
		}
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"mage -d " + a.projectDir + " build", "systemd-analyze verify " + filepath.Join(a.systemdDir, "olcrtc.service"), "systemctl daemon-reload", "systemctl enable olcrtc.service"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command %q not called:\n%s", want, joined)
		}
	}
}

func TestBuildFallsBackToGoRunMage(t *testing.T) {
	a := testApp(t)
	var calls []string
	a.envRunner = func(environment []string, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if name == "mage" {
			return nil, &exec.Error{Name: "mage", Err: exec.ErrNotFound}
		}
		if err := os.MkdirAll(filepath.Join(a.projectDir, "build"), 0755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(a.binaryFile(), []byte("binary"), 0755)
	}
	if err := a.buildOLCRTC(); err != nil {
		t.Fatal(err)
	}
	want := "go run github.com/magefile/mage@latest -d " + a.projectDir + " build"
	if !strings.Contains(strings.Join(calls, "\n"), want) {
		t.Fatalf("fallback not called: %v", calls)
	}
}

func TestBuildDefinesGoCachesWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("GOPATH", "")
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOCACHE", "")
	a := testApp(t)
	a.envRunner = func(environment []string, name string, args ...string) ([]byte, error) {
		values := map[string]string{}
		for _, item := range environment {
			if key, value, ok := strings.Cut(item, "="); ok {
				values[key] = value
			}
		}
		for _, key := range []string{"GOPATH", "GOMODCACHE", "GOCACHE"} {
			value := values[key]
			if value == "" {
				t.Fatalf("%s is not set", key)
			}
			if !strings.HasPrefix(value, a.dataDir) {
				t.Fatalf("%s=%q is outside data directory %q", key, value, a.dataDir)
			}
			if info, err := os.Stat(value); err != nil || !info.IsDir() {
				t.Fatalf("%s directory is not ready: %v", key, err)
			}
		}
		if err := os.MkdirAll(filepath.Join(a.projectDir, "build"), 0755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(a.binaryFile(), []byte("binary"), 0755)
	}
	if err := a.buildOLCRTC(); err != nil {
		t.Fatal(err)
	}
}

func TestLowMemoryCreatesAndEnablesSwap(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(a.memoryInfoPath, []byte("MemTotal:       2097152 kB\nSwapTotal:            0 kB\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	a.runner = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if name == "fallocate" {
			return nil, os.WriteFile(a.swapFile, []byte("swap"), 0600)
		}
		return nil, nil
	}
	if err := a.ensureBuildMemory(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{"fallocate -l 4G " + a.swapFile, "mkswap " + a.swapFile, "swapon " + a.swapFile} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command %q not called:\n%s", want, joined)
		}
	}
}

func TestExistingSwapFileIsNotOverwritten(t *testing.T) {
	a := testApp(t)
	if err := os.WriteFile(a.memoryInfoPath, []byte("MemTotal:       2097152 kB\nSwapTotal:            0 kB\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.swapFile, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	a.runner = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		return nil, nil
	}
	if err := a.ensureBuildMemory(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(a.swapFile)
	if err != nil || string(content) != "existing" {
		t.Fatalf("existing swap file changed: %q, %v", content, err)
	}
	if got := strings.Join(calls, "\n"); got != "swapon "+a.swapFile {
		t.Fatalf("unexpected commands: %s", got)
	}
}

func TestNormalizedServiceName(t *testing.T) {
	for _, test := range []struct{ in, want string }{{"olcrtc", "olcrtc.service"}, {"olcrtc@server.service", "olcrtc@server.service"}} {
		got, err := normalizedServiceName(test.in)
		if err != nil || got != test.want {
			t.Errorf("normalizedServiceName(%q) = %q, %v; want %q", test.in, got, err, test.want)
		}
	}
	for _, invalid := range []string{"", "../olcrtc", "olcrtc server"} {
		if _, err := normalizedServiceName(invalid); err == nil {
			t.Errorf("invalid service name %q accepted", invalid)
		}
	}
}

func TestSystemdPathsMustBeAbsoluteAndUnambiguous(t *testing.T) {
	for _, invalid := range []string{"olcrtc", "/opt/olc rtc", "/opt/olcrtc\ninvalid"} {
		if _, err := systemdAbsolutePath("test", invalid); err == nil {
			t.Errorf("invalid systemd path %q accepted", invalid)
		}
	}
	absolute := t.TempDir()
	if got, err := systemdAbsolutePath("test", absolute); err != nil || got != filepath.Clean(absolute) {
		t.Fatalf("absolute path rejected: %q, %v", got, err)
	}
}

func TestOLCRTCBinaryNameMatchesMageOutput(t *testing.T) {
	for _, test := range []struct{ goos, goarch, want string }{
		{"linux", "amd64", "olcrtc-linux-amd64"},
		{"linux", "arm64", "olcrtc-linux-arm64"},
		{"windows", "amd64", "olcrtc-windows-amd64.exe"},
	} {
		if got := olcrtcBinaryName(test.goos, test.goarch); got != test.want {
			t.Errorf("olcrtcBinaryName(%q, %q) = %q; want %q", test.goos, test.goarch, got, test.want)
		}
	}
}

func TestInitialPageHidesPanelAndServesAtRoot(t *testing.T) {
	a := testApp(t)
	root := request(a, http.MethodGet, "/", "", nil)
	if root.Code != http.StatusOK || !strings.Contains(root.Body.String(), `id="panel" hidden`) {
		t.Fatalf("root page: %d %s", root.Code, root.Body.String())
	}
	css := request(a, http.MethodGet, "/style.css", "", nil)
	if css.Code != http.StatusOK || !strings.Contains(css.Body.String(), `[hidden]`) || !strings.Contains(css.Body.String(), `display: none !important`) {
		t.Fatalf("hidden rule missing: %d %s", css.Code, css.Body.String())
	}
	legacy := request(a, http.MethodGet, "/web/", "", nil)
	if legacy.Code != http.StatusPermanentRedirect || legacy.Header().Get("Location") != "/" {
		t.Fatalf("legacy URL: %d %q", legacy.Code, legacy.Header().Get("Location"))
	}
}

