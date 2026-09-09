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
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

type Settings struct {
	Mode          string            `json:"mode"`
	Provider      string            `json:"provider"`
	ProviderToken string            `json:"providerToken"`
	Transport     string            `json:"transport"`
	RoomID        string            `json:"roomId"`
	RoomChannel   string            `json:"roomChannel"`
	CryptoKey     string            `json:"cryptoKey"`
	CryptoKeyFile string            `json:"cryptoKeyFile"`
	DNS           string            `json:"dns"`
	DataDir       string            `json:"dataDir"`
	Debug         bool              `json:"debug"`
	Engine        EngineSettings    `json:"engine"`
	SOCKS         SOCKSSettings     `json:"socks"`
	Video         VideoSettings     `json:"video"`
	VP8           VP8Settings       `json:"vp8"`
	SEI           SEISettings       `json:"sei"`
	Liveness      LivenessSettings  `json:"liveness"`
	Lifecycle     LifecycleSettings `json:"lifecycle"`
	Traffic       TrafficSettings   `json:"traffic"`
}

type EngineSettings struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type SOCKSSettings struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	Pass      string `json:"pass"`
	ProxyAddr string `json:"proxyAddr"`
	ProxyPort int    `json:"proxyPort"`
	ProxyUser string `json:"proxyUser"`
	ProxyPass string `json:"proxyPass"`
}

type VideoSettings struct {
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	FPS        int    `json:"fps"`
	QRSize     int    `json:"qrSize"`
	QRRecovery string `json:"qrRecovery"`
	Codec      string `json:"codec"`
	TileModule int    `json:"tileModule"`
	TileRS     int    `json:"tileRs"`
}

type VP8Settings struct {
	FPS       int `json:"fps"`
	BatchSize int `json:"batchSize"`
}

type SEISettings struct {
	FPS          int `json:"fps"`
	BatchSize    int `json:"batchSize"`
	FragmentSize int `json:"fragmentSize"`
	AckTimeoutMS int `json:"ackTimeoutMs"`
}

type LivenessSettings struct {
	Interval string `json:"interval"`
	Timeout  string `json:"timeout"`
	Failures int    `json:"failures"`
}

type LifecycleSettings struct {
	MaxSessionDuration string `json:"maxSessionDuration"`
}

type TrafficSettings struct {
	MaxPayloadSize int    `json:"maxPayloadSize"`
	MinDelay       string `json:"minDelay"`
	MaxDelay       string `json:"maxDelay"`
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
	dataDir, projectDir, configPath, repository, service, systemdDir string
	secureCookies                                                    bool
	mu                                                               sync.Mutex
	sessions                                                         map[string]session
	attempts                                                         map[string]attempt
	updateMu                                                         sync.Mutex
	runner                                                           func(string, ...string) ([]byte, error)
	envRunner                                                        func([]string, string, ...string) ([]byte, error)
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
	// Fetching dependencies and compiling OLC RTC can take several minutes on a
	// small VPS, so keep the response open for the complete installation request.
	s := &http.Server{Addr: addr, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 60 * time.Second}
	log.Fatal(s.ListenAndServe())
}

func newApp() *App {
	return &App{dataDir: env("OLCRTC_WEB_DATA", "./data"), projectDir: env("OLCRTC_DIR", "/opt/olcrtc"), configPath: os.Getenv("OLCRTC_CONFIG"), repository: env("OLCRTC_REPOSITORY", "https://github.com/openlibrecommunity/olcrtc.git"), service: env("OLCRTC_SERVICE", "olcrtc"), systemdDir: env("OLCRTC_SYSTEMD_DIR", "/etc/systemd/system"), secureCookies: env("OLCRTC_WEB_SECURE_COOKIE", "false") == "true", sessions: map[string]session{}, attempts: map[string]attempt{}, runner: run, envRunner: runWithEnv}
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
	// Serve the contents of the embedded web directory at the site root. Keeping
	// the directory itself in the URL used to make / show a directory listing and
	// encouraged opening /web/ directly.
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.HandleFunc("GET /web", redirectToRoot)
	mux.HandleFunc("GET /web/", redirectToRoot)
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	return securityHeaders(mux)
}

func redirectToRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusPermanentRedirect)
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
	providers := map[string]bool{"jitsi": true, "telemost": true, "wbstream": true, "none": true}
	transports := map[string]bool{"datachannel": true, "vp8channel": true, "seichannel": true, "videochannel": true}
	if s.Mode != "srv" && s.Mode != "cnc" {
		return errors.New("режим должен быть srv или cnc")
	}
	if !providers[s.Provider] {
		return errors.New("неподдерживаемый провайдер")
	}
	if !transports[s.Transport] {
		return errors.New("неподдерживаемый транспорт")
	}
	if s.Provider == "telemost" && (s.Transport == "datachannel" || s.Transport == "seichannel") {
		return errors.New("Телемост не поддерживает выбранный транспорт")
	}
	if s.Provider == "wbstream" && s.Transport == "datachannel" && s.ProviderToken == "" {
		return errors.New("WB Stream с datachannel требует auth.token с правом canPublishData")
	}
	if s.Provider != "none" && strings.TrimSpace(s.RoomID) == "" {
		return errors.New("укажите комнату")
	}
	if s.Provider == "none" {
		engines := map[string]bool{"livekit": true, "goolom": true, "jitsi": true}
		if !engines[s.Engine.Name] || s.Engine.URL == "" || s.Engine.Token == "" {
			return errors.New("для режима без провайдера укажите engine.name, engine.url и engine.token")
		}
	}
	if (s.CryptoKey == "") == (s.CryptoKeyFile == "") {
		return errors.New("укажите либо crypto.key, либо crypto.key_file")
	}
	if s.CryptoKey != "" {
		key, err := hex.DecodeString(s.CryptoKey)
		if err != nil || len(key) != 32 {
			return errors.New("crypto.key должен содержать 64 hex-символа")
		}
	}
	if s.DNS == "" {
		return errors.New("укажите DNS-сервер")
	}
	if err := validateHostPort(s.DNS, "DNS-сервер"); err != nil {
		return err
	}
	if s.Mode == "cnc" {
		if s.SOCKS.Host == "" {
			return errors.New("укажите адрес локального SOCKS5")
		}
		if err := validatePort(s.SOCKS.Port, "порт SOCKS5"); err != nil {
			return err
		}
		if !isLoopback(s.SOCKS.Host) && (s.SOCKS.User == "" || s.SOCKS.Pass == "") {
			return errors.New("для SOCKS5 не на loopback нужны логин и пароль")
		}
	}
	if s.SOCKS.ProxyAddr != "" {
		if err := validatePort(s.SOCKS.ProxyPort, "порт upstream SOCKS5"); err != nil {
			return err
		}
	} else if s.SOCKS.ProxyPort != 0 {
		return errors.New("для порта upstream SOCKS5 укажите адрес")
	}
	if err := validateTransportSettings(s); err != nil {
		return err
	}
	if err := validatePositiveDuration(s.Liveness.Interval, "интервал liveness"); err != nil {
		return err
	}
	if err := validatePositiveDuration(s.Liveness.Timeout, "таймаут liveness"); err != nil {
		return err
	}
	if s.Liveness.Failures < 0 {
		return errors.New("число пропусков liveness не может быть отрицательным")
	}
	if s.Lifecycle.MaxSessionDuration != "" {
		if err := validatePositiveDuration(s.Lifecycle.MaxSessionDuration, "длительность сессии"); err != nil {
			return err
		}
	}
	if s.Traffic.MaxPayloadSize < 0 || (s.Traffic.MaxPayloadSize > 0 && s.Traffic.MaxPayloadSize < 53) {
		return errors.New("лимит payload должен быть 0 или не меньше 53 байт")
	}
	minDelay, err := parseNonNegativeDuration(s.Traffic.MinDelay, "минимальная задержка")
	if err != nil {
		return err
	}
	maxDelay, err := parseNonNegativeDuration(s.Traffic.MaxDelay, "максимальная задержка")
	if err != nil {
		return err
	}
	if maxDelay > 0 && maxDelay < minDelay {
		return errors.New("максимальная задержка должна быть не меньше минимальной")
	}
	for _, value := range []string{s.ProviderToken, s.RoomID, s.RoomChannel, s.CryptoKeyFile, s.DataDir, s.Engine.Name, s.Engine.URL, s.Engine.Token, s.SOCKS.Host, s.SOCKS.User, s.SOCKS.Pass, s.SOCKS.ProxyAddr, s.SOCKS.ProxyUser, s.SOCKS.ProxyPass} {
		if strings.ContainsRune(value, '\x00') || len(value) > 4096 {
			return errors.New("текстовое поле содержит недопустимое значение")
		}
	}
	return nil
}

func validateTransportSettings(s Settings) error {
	switch s.Transport {
	case "vp8channel":
		if err := validateFPS(s.VP8.FPS); err != nil {
			return err
		}
		if s.VP8.BatchSize < 1 {
			return errors.New("vp8.batch_size должен быть больше нуля")
		}
	case "seichannel":
		if err := validateFPS(s.SEI.FPS); err != nil {
			return err
		}
		if s.SEI.BatchSize < 1 || s.SEI.FragmentSize < 1 || s.SEI.FragmentSize > 60000 || s.SEI.AckTimeoutMS < 1 {
			return errors.New("проверьте batch_size, fragment_size (1..60000) и ack_timeout_ms транспорта SEI")
		}
	case "videochannel":
		if s.Video.Codec != "qrcode" && s.Video.Codec != "tile" {
			return errors.New("video.codec должен быть qrcode или tile")
		}
		if s.Video.Width < 16 || s.Video.Width > 8192 || s.Video.Height < 16 || s.Video.Height > 8192 {
			return errors.New("размер видео должен быть от 16 до 8192 пикселей")
		}
		if err := validateFPS(s.Video.FPS); err != nil {
			return err
		}
		if s.Video.QRSize < 0 || s.Video.TileModule < 0 || s.Video.TileModule > 270 || s.Video.TileRS < 0 || s.Video.TileRS > 200 {
			return errors.New("проверьте параметры QR и tile")
		}
		recovery := map[string]bool{"low": true, "medium": true, "high": true, "highest": true}
		if !recovery[s.Video.QRRecovery] {
			return errors.New("неподдерживаемый уровень коррекции QR")
		}
		if s.Video.Codec == "tile" && (s.Video.Width != 1080 || s.Video.Height != 1080) {
			return errors.New("кодек tile требует размер 1080x1080")
		}
	}
	return nil
}

func validateFPS(value int) error {
	if value < 1 || value > 240 {
		return errors.New("FPS должен быть от 1 до 240")
	}
	return nil
}

func validatePort(value int, name string) error {
	if value < 1 || value > 65535 {
		return fmt.Errorf("%s должен быть от 1 до 65535", name)
	}
	return nil
}

func validateHostPort(value, name string) error {
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf("%s должен быть в формате host:port", name)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%s содержит недопустимый порт", name)
	}
	return nil
}

func validatePositiveDuration(value, name string) error {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fmt.Errorf("%s должен быть положительным интервалом, например 10s или 6h", name)
	}
	return nil
}

func parseNonNegativeDuration(value, name string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("%s должна быть интервалом не меньше нуля", name)
	}
	return d, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func defaultSettings() Settings {
	return Settings{
		Mode: "srv", Provider: "jitsi", Transport: "datachannel", DNS: "8.8.8.8:53",
		SOCKS:    SOCKSSettings{Host: "127.0.0.1", Port: 8808},
		Video:    VideoSettings{Width: 1920, Height: 1080, FPS: 30, QRRecovery: "low", Codec: "qrcode", TileModule: 4},
		VP8:      VP8Settings{FPS: 30, BatchSize: 64},
		SEI:      SEISettings{FPS: 30, BatchSize: 64, FragmentSize: 900, AckTimeoutMS: 2000},
		Liveness: LivenessSettings{Interval: "10s", Timeout: "15s", Failures: 4},
	}
}
func (a *App) loadSettings() (Settings, error) {
	s := defaultSettings()
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
	configPath := a.configFile()
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return err
	}
	return atomicWrite(configPath, []byte(settingsYAML(s)), 0600)
}

func (a *App) configFile() string {
	if a.configPath == "" {
		return filepath.Join(a.projectDir, "olcrtc.yaml")
	}
	if filepath.IsAbs(a.configPath) {
		return a.configPath
	}
	return filepath.Join(a.projectDir, a.configPath)
}

func settingsYAML(s Settings) string {
	var b strings.Builder
	line := func(indent int, key, value string) {
		b.WriteString(strings.Repeat("  ", indent))
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	section := func(name string) { b.WriteString(name + ":\n") }
	quoted := strconv.Quote

	b.WriteString("# Generated by olcrtcwebgui. Changes made here may be overwritten.\n")
	line(0, "mode", s.Mode)
	section("auth")
	line(1, "provider", s.Provider)
	if s.ProviderToken != "" {
		line(1, "token", quoted(s.ProviderToken))
	}
	if s.RoomID != "" || s.RoomChannel != "" {
		section("room")
		if s.RoomID != "" {
			line(1, "id", quoted(s.RoomID))
		}
		if s.RoomChannel != "" {
			line(1, "channel", quoted(s.RoomChannel))
		}
	}
	section("crypto")
	if s.CryptoKey != "" {
		line(1, "key", quoted(s.CryptoKey))
	} else {
		line(1, "key_file", quoted(s.CryptoKeyFile))
	}
	section("net")
	line(1, "transport", s.Transport)
	line(1, "dns", quoted(s.DNS))
	if s.Provider == "none" {
		section("engine")
		line(1, "name", s.Engine.Name)
		line(1, "url", quoted(s.Engine.URL))
		line(1, "token", quoted(s.Engine.Token))
	}
	writeSOCKSYAML(&b, s)
	writeTransportYAML(&b, s)
	section("liveness")
	line(1, "interval", quoted(s.Liveness.Interval))
	line(1, "timeout", quoted(s.Liveness.Timeout))
	line(1, "failures", strconv.Itoa(s.Liveness.Failures))
	if s.Lifecycle.MaxSessionDuration != "" {
		section("lifecycle")
		line(1, "max_session_duration", quoted(s.Lifecycle.MaxSessionDuration))
	}
	if s.Traffic.MaxPayloadSize != 0 || s.Traffic.MinDelay != "" || s.Traffic.MaxDelay != "" {
		section("traffic")
		line(1, "max_payload_size", strconv.Itoa(s.Traffic.MaxPayloadSize))
		if s.Traffic.MinDelay != "" {
			line(1, "min_delay", quoted(s.Traffic.MinDelay))
		}
		if s.Traffic.MaxDelay != "" {
			line(1, "max_delay", quoted(s.Traffic.MaxDelay))
		}
	}
	if s.DataDir != "" {
		line(0, "data", quoted(s.DataDir))
	}
	line(0, "debug", strconv.FormatBool(s.Debug))
	return b.String()
}

func writeSOCKSYAML(b *strings.Builder, s Settings) {
	if s.Mode == "cnc" {
		b.WriteString("socks:\n")
		fmt.Fprintf(b, "  host: %s\n  port: %d\n", strconv.Quote(s.SOCKS.Host), s.SOCKS.Port)
		if s.SOCKS.User != "" {
			fmt.Fprintf(b, "  user: %s\n  pass: %s\n", strconv.Quote(s.SOCKS.User), strconv.Quote(s.SOCKS.Pass))
		}
		return
	}
	if s.Mode == "srv" && s.SOCKS.ProxyAddr != "" {
		b.WriteString("socks:\n")
		fmt.Fprintf(b, "  proxy_addr: %s\n  proxy_port: %d\n", strconv.Quote(s.SOCKS.ProxyAddr), s.SOCKS.ProxyPort)
		if s.SOCKS.ProxyUser != "" {
			fmt.Fprintf(b, "  proxy_user: %s\n  proxy_pass: %s\n", strconv.Quote(s.SOCKS.ProxyUser), strconv.Quote(s.SOCKS.ProxyPass))
		}
	}
}

func writeTransportYAML(b *strings.Builder, s Settings) {
	switch s.Transport {
	case "vp8channel":
		fmt.Fprintf(b, "vp8:\n  fps: %d\n  batch_size: %d\n", s.VP8.FPS, s.VP8.BatchSize)
	case "seichannel":
		fmt.Fprintf(b, "sei:\n  fps: %d\n  batch_size: %d\n  fragment_size: %d\n  ack_timeout_ms: %d\n", s.SEI.FPS, s.SEI.BatchSize, s.SEI.FragmentSize, s.SEI.AckTimeoutMS)
	case "videochannel":
		fmt.Fprintf(b, "video:\n  codec: %s\n  width: %d\n  height: %d\n  fps: %d\n", s.Video.Codec, s.Video.Width, s.Video.Height, s.Video.FPS)
		fmt.Fprintf(b, "  qr_size: %d\n  qr_recovery: %s\n  tile_module: %d\n  tile_rs: %d\n", s.Video.QRSize, s.Video.QRRecovery, s.Video.TileModule, s.Video.TileRS)
	}
}

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	local, gitErr := a.git("rev-parse", "HEAD")
	if gitErr != nil {
		local = nil
	}
	active := false
	status := "не установлен"
	installed := a.installationReady()
	if installed {
		status = "остановлен"
		serviceName, _ := normalizedServiceName(a.service)
		if _, e := a.runner("systemctl", "is-active", "--quiet", serviceName); e == nil {
			active = true
			status = "работает"
		}
	} else if strings.TrimSpace(string(local)) != "" {
		status = "требуется сборка"
	}
	jsonOut(w, 200, map[string]any{"installed": installed, "active": active, "status": status, "version": short(local), "projectDir": a.projectDir, "configPath": a.configFile(), "repository": a.repository})
}
func (a *App) serviceAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "start" && action != "stop" && action != "restart" {
		apiError(w, 400, "Неизвестное действие")
		return
	}
	if !a.installationReady() {
		apiError(w, http.StatusConflict, "Сервис не установлен. Нажмите «Установить / обновить»")
		return
	}
	serviceName, err := normalizedServiceName(a.service)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := a.runner("systemctl", action, serviceName)
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
	local, localErr := a.git("rev-parse", "HEAD")
	if localErr != nil {
		local = nil
	}
	needsInstall := !a.installationReady()
	jsonOut(w, 200, map[string]any{"current": short(local), "latest": short([]byte(fields[0])), "available": strings.TrimSpace(string(local)) != fields[0] || needsInstall, "needsInstall": needsInstall})
}
func (a *App) installUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.updateMu.TryLock() {
		apiError(w, 409, "Обновление уже выполняется")
		return
	}
	defer a.updateMu.Unlock()
	if _, e := os.Stat(filepath.Join(a.projectDir, ".git")); errors.Is(e, os.ErrNotExist) {
		// The panel may already have created olcrtc.yaml in this directory. Initialising
		// and fetching in place supports that valid case, unlike `git clone`,
		// which rejects a non-empty destination.
		if err := os.MkdirAll(a.projectDir, 0755); err != nil {
			apiError(w, 500, err.Error())
			return
		}
		for _, command := range [][]string{{"init"}, {"remote", "add", "origin", a.repository}, {"fetch", "--depth=1", "origin", "HEAD"}, {"checkout", "-B", "olcrtc-upstream", "FETCH_HEAD"}} {
			out, err := a.git(command...)
			if err != nil {
				apiError(w, 500, commandError(out, err))
				return
			}
		}
	} else {
		out, err := a.git("fetch", "origin", "HEAD")
		if err == nil {
			out, err = a.git("merge", "--ff-only", "FETCH_HEAD")
		}
		if err != nil {
			apiError(w, 500, commandError(out, err))
			return
		}
	}
	if s, err := a.loadSettings(); err == nil {
		if err := validateSettings(s); err == nil {
			if err := a.writeSettings(s); err != nil {
				apiError(w, 500, err.Error())
				return
			}
		}
	}
	if err := a.buildOLCRTC(); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	if err := a.installService(); err != nil {
		apiError(w, 500, err.Error())
		return
	}
	jsonOut(w, 200, map[string]any{"ok": true, "message": "OLC RTC обновлён, собран и установлен как systemd-сервис"})
}

func (a *App) buildOLCRTC() error {
	buildEnv, err := a.buildEnvironment()
	if err != nil {
		return err
	}
	out, err := a.envRunner(buildEnv, "mage", "-d", a.projectDir, "build")
	if err != nil && commandNotFound(err) {
		out, err = a.envRunner(buildEnv, "go", "run", "github.com/magefile/mage@latest", "-d", a.projectDir, "build")
	}
	if err != nil {
		return fmt.Errorf("сборка OLC RTC: %s", commandError(out, err))
	}
	info, err := os.Stat(a.binaryFile())
	if err != nil {
		return fmt.Errorf("сборка завершилась без файла %s: %w", a.binaryFile(), err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("результат сборки не является файлом: %s", a.binaryFile())
	}
	return nil
}

func (a *App) buildEnvironment() ([]string, error) {
	cacheRoot, err := filepath.Abs(env("OLCRTC_GO_CACHE", filepath.Join(a.dataDir, "go-cache")))
	if err != nil {
		return nil, fmt.Errorf("определить каталог кэша Go: %w", err)
	}
	goPath := filepath.Join(cacheRoot, "gopath")
	modCache := filepath.Join(goPath, "pkg", "mod")
	goCache := filepath.Join(cacheRoot, "build")
	for _, directory := range []string{goPath, modCache, goCache} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			return nil, fmt.Errorf("создать каталог кэша Go %s: %w", directory, err)
		}
	}
	environment := os.Environ()
	environment = replaceEnv(environment, "GOPATH", goPath)
	environment = replaceEnv(environment, "GOMODCACHE", modCache)
	environment = replaceEnv(environment, "GOCACHE", goCache)
	return environment, nil
}

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

func commandNotFound(err error) bool {
	var execErr *exec.Error
	return errors.Is(err, exec.ErrNotFound) || (errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound))
}

func (a *App) installService() error {
	serviceName, err := normalizedServiceName(a.service)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.systemdDir, 0755); err != nil {
		return fmt.Errorf("создать каталог systemd: %w", err)
	}
	unit := fmt.Sprintf(`[Unit]
Description=OLC RTC tunnel
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, systemdQuote(a.projectDir), systemdQuote(a.binaryFile()), systemdQuote(a.configFile()))
	if err := atomicWrite(filepath.Join(a.systemdDir, serviceName), []byte(unit), 0644); err != nil {
		return fmt.Errorf("установить systemd-сервис: %w", err)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", serviceName}} {
		out, err := a.runner("systemctl", args...)
		if err != nil {
			return fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), commandError(out, err))
		}
	}
	return nil
}

func normalizedServiceName(name string) (string, error) {
	if name == "" {
		return "", errors.New("имя systemd-сервиса не задано")
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("_.@-", char) {
			continue
		}
		return "", errors.New("имя systemd-сервиса содержит недопустимые символы")
	}
	if !strings.HasSuffix(name, ".service") {
		name += ".service"
	}
	return name, nil
}

func systemdQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func (a *App) binaryFile() string {
	return filepath.Join(a.projectDir, "build", "olcrtc")
}

func (a *App) serviceFile() string {
	name, err := normalizedServiceName(a.service)
	if err != nil {
		return ""
	}
	return filepath.Join(a.systemdDir, name)
}

func (a *App) installationReady() bool {
	binary, binaryErr := os.Stat(a.binaryFile())
	unit, unitErr := os.Stat(a.serviceFile())
	return binaryErr == nil && binary.Mode().IsRegular() && unitErr == nil && unit.Mode().IsRegular()
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
	return runWithEnv(os.Environ(), name, args...)
}

func runWithEnv(environment []string, name string, args ...string) ([]byte, error) {
	command := exec.Command(name, args...)
	command.Env = environment
	return command.CombinedOutput()
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

