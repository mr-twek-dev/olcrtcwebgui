package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

type Settings struct {
	Provider      string `json:"provider"`
	Transport     string `json:"transport"`
	Domain        string `json:"domain"`
	ListenAddress string `json:"listenAddress"`
	ListenPort    int    `json:"listenPort"`
	Upstream      string `json:"upstream"`
	AutoStart     bool   `json:"autoStart"`
}

type credentials struct{ Username, Salt, Hash string }
type session struct {
	User    string
	Expires time.Time
}
type attempt struct {
	Count int
	Since time.Time
}

type App struct {
	dataDir, projectDir, repository, service string
	secureCookies                            bool
	mu                                       sync.Mutex
	sessions                                 map[string]session
	attempts                                 map[string]attempt
	updateMu                                 sync.Mutex
	runner                                   func(string, ...string) ([]byte, error)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	setPassword := flag.Bool("set-password", false, "set or replace the administrator credentials")
	username := flag.String("username", "admin", "administrator username used with -set-password")
	flag.Parse()
	a := newApp()
	if *setPassword {
		password := os.Getenv("OLCRTC_WEB_ADMIN_PASSWORD")
		if password == "" {
			log.Fatal("OLCRTC_WEB_ADMIN_PASSWORD must be set")
		}
		if err := a.saveCredentials(*username, password); err != nil {
			log.Fatal(err)
		}
		log.Printf("credentials updated for %s", *username)
		return
	}
	if _, err := a.loadCredentials(); errors.Is(err, os.ErrNotExist) {
		password := os.Getenv("OLCRTC_WEB_ADMIN_PASSWORD")
		if password == "" {
			log.Fatal("first start: set OLCRTC_WEB_ADMIN_PASSWORD")
		}
		if err := a.saveCredentials(env("OLCRTC_WEB_ADMIN_USERNAME", "admin"), password); err != nil {
			log.Fatal(err)
		}
	} else if err != nil {
		log.Fatal(err)
	}
	addr := env("OLCRTC_WEB_ADDR", "127.0.0.1:8080")
	log.Printf("OLC RTC web panel listening on %s", addr)
	s := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func newApp() *App {
	return &App{dataDir: env("OLCRTC_WEB_DATA", "./data"), projectDir: env("OLCRTC_DIR", "/opt/olcrtc"), repository: env("OLCRTC_REPOSITORY", "https://github.com/openlibrecommunity/olcrtc.git"), service: env("OLCRTC_SERVICE", "olcrtc"), secureCookies: env("OLCRTC_WEB_SECURE_COOKIE", "false") == "true", sessions: map[string]session{}, attempts: map[string]attempt{}, runner: run}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", a.auth(a.logout))
	mux.HandleFunc("GET /api/me", a.auth(a.me))
	mux.HandleFunc("GET /api/settings", a.auth(a.settings))
	mux.HandleFunc("PUT /api/settings", a.auth(a.saveSettings))
	mux.HandleFunc("GET /api/status", a.auth(a.status))
	mux.HandleFunc("POST /api/service/{action}", a.auth(a.serviceAction))
	mux.HandleFunc("POST /api/update/check", a.auth(a.checkUpdate))
	mux.HandleFunc("POST /api/update/install", a.auth(a.installUpdate))
	mux.Handle("/", http.FileServer(http.FS(webFS)))
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("olcrtc_session")
		if err != nil {
			apiError(w, http.StatusUnauthorized, "Требуется вход")
			return
		}
		a.mu.Lock()
		s, ok := a.sessions[c.Value]
		if ok && time.Now().After(s.Expires) {
			delete(a.sessions, c.Value)
			ok = false
		}
		a.mu.Unlock()
		if !ok {
			apiError(w, http.StatusUnauthorized, "Сессия истекла")
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(r) {
			apiError(w, http.StatusForbidden, "Недопустимый источник запроса")
			return
		}
		r.Header.Set("X-Authenticated-User", s.User)
		next(w, r)
	}
}

func sameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || o == "http://"+r.Host || o == "https://"+r.Host
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, 403, "Недопустимый источник запроса")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.mu.Lock()
	at := a.attempts[ip]
	if time.Since(at.Since) > 10*time.Minute {
		at = attempt{Since: time.Now()}
	}
	if at.Count >= 8 {
		a.mu.Unlock()
		apiError(w, 429, "Слишком много попыток. Повторите позже")
		return
	}
	a.mu.Unlock()
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	c, err := a.loadCredentials()
	valid := err == nil && subtle.ConstantTimeCompare([]byte(c.Hash), []byte(passwordHash(in.Password, c.Salt))) == 1 && subtle.ConstantTimeCompare([]byte(c.Username), []byte(in.Username)) == 1
	if !valid {
		a.mu.Lock()
		at.Count++
		if at.Since.IsZero() {
			at.Since = time.Now()
		}
		a.attempts[ip] = at
		a.mu.Unlock()
		time.Sleep(250 * time.Millisecond)
		apiError(w, 401, "Неверный логин или пароль")
		return
	}
	token := random(32)
	a.mu.Lock()
	delete(a.attempts, ip)
	a.sessions[token] = session{User: c.Username, Expires: time.Now().Add(12 * time.Hour)}
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "olcrtc_session", Value: token, Path: "/", HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	jsonOut(w, 200, map[string]any{"username": c.Username})
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("olcrtc_session"); e == nil {
		a.mu.Lock()
		delete(a.sessions, c.Value)
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "olcrtc_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) me(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, map[string]string{"username": r.Header.Get("X-Authenticated-User")})
}

func (a *App) settings(w http.ResponseWriter, r *http.Request) {
	s, err := a.loadSettings()
	if err != nil {
		apiError(w, 500, err.Error())
		return
	}
	jsonOut(w, 200, s)
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if !decode(w, r, &s) {
		return
	}
	if err := validateSettings(s); err != nil {
		apiError(w, 400, err.Error())
		return
	}
	if err := a.writeSettings(s); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	jsonOut(w, 200, s)
}
func validateSettings(s Settings) error {
	allowed := regexp.MustCompile(`^[a-zA-Z0-9._:/-]*$`)
	if !allowed.MatchString(s.Provider) || !allowed.MatchString(s.Transport) || !allowed.MatchString(s.Domain) || !allowed.MatchString(s.ListenAddress) || !allowed.MatchString(s.Upstream) {
		return errors.New("поля содержат недопустимые символы")
	}
	if s.Provider == "" || s.Transport == "" {
		return errors.New("выберите провайдера и транспорт")
	}
	if s.ListenPort < 1 || s.ListenPort > 65535 {
		return errors.New("порт должен быть от 1 до 65535")
	}
	return nil
}
func defaultSettings() Settings {
	return Settings{Provider: "cloudflare", Transport: "websocket", ListenAddress: "0.0.0.0", ListenPort: 8443, AutoStart: true}
}
func (a *App) loadSettings() (Settings, error) {
	var s Settings
	b, e := os.ReadFile(filepath.Join(a.dataDir, "settings.json"))
	if errors.Is(e, os.ErrNotExist) {
		return defaultSettings(), nil
	}
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	return s, e
}
func (a *App) writeSettings(s Settings) error {
	if err := os.MkdirAll(a.dataDir, 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := atomicWrite(filepath.Join(a.dataDir, "settings.json"), b, 0600); err != nil {
		return err
	}
	if err := os.MkdirAll(a.projectDir, 0755); err != nil {
		return err
	}
	envText := fmt.Sprintf("# Generated by olcrtcwebgui. Do not edit manually.\nOLCRTC_PROVIDER=%s\nOLCRTC_TRANSPORT=%s\nOLCRTC_DOMAIN=%s\nOLCRTC_LISTEN_ADDRESS=%s\nOLCRTC_LISTEN_PORT=%d\nOLCRTC_UPSTREAM=%s\n", s.Provider, s.Transport, s.Domain, s.ListenAddress, s.ListenPort, s.Upstream)
	return atomicWrite(filepath.Join(a.projectDir, ".env"), []byte(envText), 0600)
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	local, _ := a.git("rev-parse", "HEAD")
	active := false
	status := "не установлен"
	if _, e := os.Stat(filepath.Join(a.projectDir, ".git")); e == nil {
		status = "остановлен"
		if e := exec.Command("systemctl", "is-active", "--quiet", a.service).Run(); e == nil {
			active = true
			status = "работает"
		}
	}
	jsonOut(w, 200, map[string]any{"installed": strings.TrimSpace(string(local)) != "", "active": active, "status": status, "version": short(local), "projectDir": a.projectDir, "repository": a.repository})
}
func (a *App) serviceAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "start" && action != "stop" && action != "restart" {
		apiError(w, 400, "Неизвестное действие")
		return
	}
	out, err := a.runner("systemctl", action, a.service)
	if err != nil {
		apiError(w, 500, commandError(out, err))
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) checkUpdate(w http.ResponseWriter, r *http.Request) {
	remote, err := a.runner("git", "ls-remote", a.repository, "HEAD")
	if err != nil {
		apiError(w, 502, commandError(remote, err))
		return
	}
	fields := strings.Fields(string(remote))
	if len(fields) == 0 {
		apiError(w, 502, "Репозиторий не вернул версию")
		return
	}
	local, _ := a.git("rev-parse", "HEAD")
	jsonOut(w, 200, map[string]any{"current": short(local), "latest": short([]byte(fields[0])), "available": strings.TrimSpace(string(local)) != fields[0]})
}
func (a *App) installUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.updateMu.TryLock() {
		apiError(w, 409, "Обновление уже выполняется")
		return
	}
	defer a.updateMu.Unlock()
	if _, e := os.Stat(filepath.Join(a.projectDir, ".git")); errors.Is(e, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(a.projectDir), 0755); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		out, err := a.runner("git", "clone", "--depth=1", a.repository, a.projectDir)
		if err != nil {
			apiError(w, 500, commandError(out, err))
			return
		}
	} else {
		out, err := a.git("pull", "--ff-only")
		if err != nil {
			apiError(w, 500, commandError(out, err))
			return
		}
	}
	s, _ := a.loadSettings()
	if err := a.writeSettings(s); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	out, err := a.compose("up", "-d", "--build")
	if err != nil {
		apiError(w, 500, commandError(out, err))
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "message": "OLC RTC обновлён и запущен"})
}
func (a *App) compose(args ...string) ([]byte, error) {
	all := append([]string{"compose", "--project-directory", a.projectDir}, args...)
	return a.runner("docker", all...)
}
func (a *App) git(args ...string) ([]byte, error) {
	all := append([]string{"-C", a.projectDir}, args...)
	return a.runner("git", all...)
}

func (a *App) saveCredentials(user, password string) error {
	if len(user) < 3 || len(password) < 12 {
		return errors.New("логин: минимум 3 символа; пароль: минимум 12 символов")
	}
	if err := os.MkdirAll(a.dataDir, 0700); err != nil {
		return err
	}
	salt := random(16)
	c := credentials{Username: user, Salt: salt, Hash: passwordHash(password, salt)}
	b, _ := json.Marshal(c)
	return atomicWrite(filepath.Join(a.dataDir, "credentials.json"), b, 0600)
}
func (a *App) loadCredentials() (credentials, error) {
	var c credentials
	b, e := os.ReadFile(filepath.Join(a.dataDir, "credentials.json"))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func passwordHash(password, salt string) string {
	s, _ := base64.RawURLEncoding.DecodeString(salt)
	v := append([]byte(password), s...)
	sum := sha256.Sum256(v)
	for i := 0; i < 210000; i++ {
		h := hmac.New(sha256.New, []byte(password))
		h.Write(sum[:])
		h.Write(s)
		sum = sha256.Sum256(h.Sum(nil))
	}
	return hex.EncodeToString(sum[:])
}
func random(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func atomicWrite(path string, b []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func run(name string, args ...string) ([]byte, error) {
	ctx := exec.Command(name, args...)
	return ctx.CombinedOutput()
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		apiError(w, 400, "Некорректный JSON")
		return false
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		apiError(w, 400, "В запросе несколько JSON-значений")
		return false
	}
	return true
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func apiError(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func commandError(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 500 {
		s = s[:500]
	}
	if s == "" {
		s = err.Error()
	}
	return s
}
func short(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}
