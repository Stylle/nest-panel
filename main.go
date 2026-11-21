package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const version = "0.2.3"

//go:embed all:web
var frontend embed.FS

type Site struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}
type Group struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Sites  []Site `json:"sites"`
	Hidden bool   `json:"hidden"`
}
type Theme struct {
	Palette        string `json:"palette"`
	Layout         string `json:"layout"`
	Icons          string `json:"icons"`
	Density        string `json:"density"`
	Background     string `json:"background"`
	Opacity        int    `json:"opacity"`
	Blur           int    `json:"blur"`
	Navigation     string `json:"navigation,omitempty"`
	Surface        string `json:"surface,omitempty"`
	RowGap         int    `json:"rowGap,omitempty"`
	ColumnGap      int    `json:"columnGap,omitempty"`
	IconSize       int    `json:"iconSize,omitempty"`
	OverlayOpacity *int   `json:"overlayOpacity,omitempty"`
	Mask           *int   `json:"mask,omitempty"`
	ShowClock      *bool  `json:"showClock,omitempty"`
}
type Config struct {
	Revision int64   `json:"revision"`
	Name     string  `json:"name"`
	Public   bool    `json:"public"`
	Theme    Theme   `json:"theme"`
	Groups   []Group `json:"groups"`
}
type Account struct {
	Username string `json:"username"`
	Hash     string `json:"hash"`
}
type App struct {
	db               *sql.DB
	mu               sync.RWMutex
	config           Config
	account          Account
	backgroundSource string
	fetchSlots       chan struct{}
	loginSlots       chan struct{}
	imageSlots       chan struct{}
	attempts         map[string]attempt
	limiterMu        sync.Mutex
}
type attempt struct {
	Count int
	Until time.Time
}

func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func defaultConfig() Config {
	return Config{Name: "栖页", Theme: Theme{Palette: "graphite", Layout: "cards", Icons: "original", Density: "standard", Opacity: 94}, Groups: []Group{{ID: newID(), Title: "常用工具", Sites: []Site{}}}}
}
func openApp(dir string) (*App, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	abs, e := filepath.Abs(dir)
	if e != nil {
		return nil, e
	}
	db, e := sql.Open("sqlite", filepath.ToSlash(filepath.Join(abs, "panel.db")))
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS icons(id TEXT PRIMARY KEY,mime TEXT NOT NULL,data BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,expires INTEGER NOT NULL);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	a := &App{db: db, config: defaultConfig(), fetchSlots: make(chan struct{}, 3), loginSlots: make(chan struct{}, 2), imageSlots: make(chan struct{}, 1), attempts: map[string]attempt{}}
	for key, target := range map[string]any{"config": &a.config, "account": &a.account, "background_source": &a.backgroundSource} {
		var raw string
		e = db.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&raw)
		if e == nil {
			if e = json.Unmarshal([]byte(raw), target); e != nil {
				db.Close()
				return nil, fmt.Errorf("invalid %s data", key)
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			db.Close()
			return nil, e
		}
	}
	if e = validateConfig(a.config); e != nil {
		db.Close()
		return nil, e
	}
	db.Exec("DELETE FROM sessions WHERE expires < ?", time.Now().Unix())
	return a, nil
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	jsonOut(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	if e := d.Decode(v); e != nil {
		fail(w, 400, "Invalid request / 请求内容无效或文件过大")
		return false
	}
	var extra any
	if d.Decode(&extra) != ioEOF {
		fail(w, 400, "Only one JSON document is allowed")
		return false
	}
	return true
}

var ioEOF = io.EOF

func strongPassword(s string) bool {
	if len(s) < 12 || len(s) > 72 {
		return false
	}
	var lo, up, num, sym bool
	for _, r := range s {
		switch {
		case unicode.IsLower(r):
			lo = true
		case unicode.IsUpper(r):
			up = true
		case unicode.IsDigit(r):
			num = true
		case !unicode.IsSpace(r):
			sym = true
		}
	}
	return lo && up && num && sym
}
func validURL(raw string) (*url.URL, error) {
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || len(raw) > 2048 {
		return nil, errors.New("Use an HTTP(S) URL without embedded credentials / 请使用不含账号密码的 HTTP(S) 地址")
	}
	return u, nil
}
func validateConfig(c Config) error {
	if c.Theme.OverlayOpacity != nil && (*c.Theme.OverlayOpacity < 0 || *c.Theme.OverlayOpacity > 100) {
		return errors.New("Invalid overlay opacity")
	}
	for _, gap := range []int{c.Theme.RowGap, c.Theme.ColumnGap} {
		if gap != 0 && (gap < 4 || gap > 40) {
			return errors.New("Invalid website grid spacing")
		}
	}
	if (c.Theme.Navigation != "" && !oneOf(c.Theme.Navigation, "top", "left")) || (c.Theme.Surface != "" && !oneOf(c.Theme.Surface, "glass", "solid")) || (c.Theme.IconSize != 0 && (c.Theme.IconSize < 28 || c.Theme.IconSize > 64)) || (c.Theme.Mask != nil && (*c.Theme.Mask < 20 || *c.Theme.Mask > 80)) {
		return errors.New("Invalid appearance settings")
	}
	if len(c.Name) == 0 || len(c.Name) > 100 || len(c.Groups) > 200 {
		return errors.New("Invalid panel name or too many groups")
	}
	if !oneOf(c.Theme.Palette, "graphite", "ivory", "ocean", "system") || !oneOf(c.Theme.Layout, "cards", "grid") || !oneOf(c.Theme.Icons, "original", "tile", "color", "glass", "floating") || !oneOf(c.Theme.Density, "compact", "standard", "relaxed") || c.Theme.Opacity < 25 || c.Theme.Opacity > 100 || c.Theme.Blur < 0 || c.Theme.Blur > 24 {
		return errors.New("Invalid theme settings")
	}
	ids := map[string]bool{}
	n := 0
	for _, g := range c.Groups {
		if g.ID == "" || len(g.ID) > 80 || ids[g.ID] || len(g.Title) == 0 || len(g.Title) > 150 {
			return errors.New("Invalid or duplicate group")
		}
		ids[g.ID] = true
		for _, s := range g.Sites {
			n++
			if s.ID == "" || len(s.ID) > 80 || ids[s.ID] || len(s.Title) == 0 || len(s.Title) > 300 || len(s.Description) > 1000 {
				return errors.New("Invalid or duplicate website")
			}
			ids[s.ID] = true
			if _, e := validURL(s.URL); e != nil {
				return e
			}
			if s.Icon != "" && !validHash(s.Icon) {
				return errors.New("Invalid icon")
			}
		}
	}
	if n > 5000 {
		return errors.New("At most 5000 websites")
	}
	if c.Theme.Background != "" && !validHash(c.Theme.Background) {
		return errors.New("Invalid background")
	}
	if raw, e := json.Marshal(c); e != nil || len(raw) > 3<<20 {
		return errors.New("Configuration metadata exceeds 3 MB")
	}
	return nil
}
func oneOf(s string, choices ...string) bool {
	for _, v := range choices {
		if s == v {
			return true
		}
	}
	return false
}
func validHash(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }
func storeJSON(tx *sql.Tx, key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, string(b))
	return e
}
func (a *App) authenticated(r *http.Request) bool {
	c, e := r.Cookie(sessionCookieName(r))
	if e != nil || len(c.Value) != 64 {
		return false
	}
	h := sha256.Sum256([]byte(c.Value))
	var expires int64
	e = a.db.QueryRow("SELECT expires FROM sessions WHERE id=?", hex.EncodeToString(h[:])).Scan(&expires)
	return e == nil && expires > time.Now().Unix()
}
func sessionCookieName(r *http.Request) string {
	h := sha256.Sum256([]byte(strings.ToLower(r.Host)))
	return "nest_session_" + hex.EncodeToString(h[:4])
}
func (a *App) issueSession(w http.ResponseWriter, r *http.Request) error {
	token := newID() + newID()
	h := sha256.Sum256([]byte(token))
	exp := time.Now().Add(7 * 24 * time.Hour)
	a.db.Exec("DELETE FROM sessions WHERE expires < ?", time.Now().Unix())
	if _, e := a.db.Exec("INSERT INTO sessions(id,expires) VALUES(?,?)", hex.EncodeToString(h[:]), exp.Unix()); e != nil {
		return e
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName(r), Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", MaxAge: 604800, Expires: exp})
	return nil
}
func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]string{"status": "ok", "version": version})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		setup := a.account.Hash == ""
		a.mu.RUnlock()
		jsonOut(w, 200, map[string]any{"setup": setup, "authenticated": a.authenticated(r), "version": version})
	})
	mux.HandleFunc("POST /api/setup", a.setup)
	mux.HandleFunc("POST /api/login", a.login)
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, e := r.Cookie(sessionCookieName(r)); e == nil {
			h := sha256.Sum256([]byte(c.Value))
			a.db.Exec("DELETE FROM sessions WHERE id=?", hex.EncodeToString(h[:]))
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName(r), Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		jsonOut(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/panel", func(w http.ResponseWriter, r *http.Request) {
		auth := a.authenticated(r)
		a.mu.RLock()
		defer a.mu.RUnlock()
		if !auth && !a.config.Public {
			fail(w, 401, "Login required")
			return
		}
		view := a.config
		if !auth {
			view.Groups = make([]Group, 0, len(a.config.Groups))
			for _, g := range a.config.Groups {
				if !g.Hidden {
					view.Groups = append(view.Groups, g)
				}
			}
		}
		jsonOut(w, 200, view)
	})
	mux.HandleFunc("PUT /api/panel", a.admin(a.saveConfig))
	mux.HandleFunc("POST /api/password", a.admin(a.password))
	mux.HandleFunc("POST /api/metadata", a.admin(a.metadata))
	mux.HandleFunc("POST /api/image", a.admin(a.uploadImage))
	mux.HandleFunc("GET /api/background", a.admin(a.backgroundStatus))
	mux.HandleFunc("POST /api/background", a.admin(a.updateBackground))
	mux.HandleFunc("GET /api/icons/{id}", a.icon)
	mux.HandleFunc("GET /api/export", a.admin(a.exportConfig))
	mux.HandleFunc("POST /api/import/preview", a.admin(a.previewImport))
	mux.HandleFunc("POST /api/import", a.admin(a.importConfig))
	mux.HandleFunc("POST /api/backup", a.admin(a.backup))
	mux.HandleFunc("POST /api/restore", a.admin(a.restore))
	mux.HandleFunc("GET /api/docker", a.admin(a.docker))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "Not found") })
	files, _ := fs.Sub(frontend, "web")
	fileHandler := http.FileServer(http.FS(files))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if _, e := fs.Stat(files, strings.TrimPrefix(r.URL.Path, "/")); e != nil {
				http.NotFound(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public,max-age=31536000,immutable")
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				if compressed, e := fs.ReadFile(files, strings.TrimPrefix(r.URL.Path, "/")+".gz"); e == nil {
					w.Header().Set("Content-Encoding", "gzip")
					w.Header().Set("Vary", "Accept-Encoding")
					w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(r.URL.Path)))
					http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(compressed))
					return
				}
			}
		}
		fileHandler.ServeHTTP(w, r)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-Requested-With") != "NestPanel" {
				fail(w, 403, "Invalid request origin")
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || u.Host != r.Host {
					fail(w, 403, "Invalid request origin")
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (a *App) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			fail(w, 401, "Login required")
			return
		}
		next(w, r)
	}
}
func (a *App) setup(w http.ResponseWriter, r *http.Request) {
	var v struct{ Username, Password string }
	if !decode(w, r, &v, 4096) {
		return
	}
	if len(v.Username) < 1 || len(v.Username) > 64 || !strongPassword(v.Password) {
		fail(w, 400, "Password needs 12–72 characters, uppercase, lowercase, number and symbol / 密码需含大小写字母、数字和符号，至少12位")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.account.Hash != "" {
		fail(w, 409, "Already initialized")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), 12)
	if e != nil {
		fail(w, 500, "Password unavailable")
		return
	}
	account := Account{v.Username, string(hash)}
	tx, e := a.db.Begin()
	if e == nil {
		e = storeJSON(tx, "account", account)
		if e == nil {
			e = storeJSON(tx, "config", a.config)
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if e != nil {
		fail(w, 500, "Cannot save account")
		return
	}
	a.account = account
	if a.issueSession(w, r) != nil {
		fail(w, 500, "Cannot create session")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	var v struct{ Username, Password string }
	if !decode(w, r, &v, 4096) {
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.limiterMu.Lock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.Until) {
			delete(a.attempts, k)
		}
	}
	t := a.attempts[ip]
	if t.Count >= 10 || len(a.attempts) > 2048 {
		a.limiterMu.Unlock()
		fail(w, 429, "Too many attempts; try again in 15 minutes")
		return
	}
	t.Count++
	if t.Until.IsZero() {
		t.Until = now.Add(15 * time.Minute)
	}
	a.attempts[ip] = t
	a.limiterMu.Unlock()
	select {
	case a.loginSlots <- struct{}{}:
		defer func() { <-a.loginSlots }()
	default:
		fail(w, 429, "Please try again shortly")
		return
	}
	a.mu.RLock()
	account := a.account
	a.mu.RUnlock()
	if len(v.Password) > 128 || bcrypt.CompareHashAndPassword([]byte(account.Hash), []byte(v.Password)) != nil || v.Username != account.Username {
		fail(w, 401, "Incorrect account or password / 账号或密码不正确")
		return
	}
	a.limiterMu.Lock()
	delete(a.attempts, ip)
	a.limiterMu.Unlock()
	if a.issueSession(w, r) != nil {
		fail(w, 500, "Cannot create session")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) saveConfig(w http.ResponseWriter, r *http.Request) {
	var c Config
	if !decode(w, r, &c, 4<<20) {
		return
	}
	if e := validateConfig(c); e != nil {
		fail(w, 400, e.Error())
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if c.Revision != a.config.Revision {
		fail(w, 409, "Settings changed elsewhere. Reload before saving / 配置已变更，请刷新后重试")
		return
	}
	c.Revision++
	tx, e := a.db.Begin()
	if e == nil {
		e = validateReferences(tx, c)
		if e == nil {
			e = storeJSON(tx, "config", c)
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if e != nil {
		fail(w, 500, "Save failed")
		return
	}
	a.config = c
	jsonOut(w, 200, c)
}
func (a *App) password(w http.ResponseWriter, r *http.Request) {
	var v struct{ Current, Username, Password string }
	if !decode(w, r, &v, 4096) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(v.Current) > 128 || bcrypt.CompareHashAndPassword([]byte(a.account.Hash), []byte(v.Current)) != nil {
		fail(w, 403, "Current password is incorrect")
		return
	}
	if !strongPassword(v.Password) || len(v.Username) < 1 || len(v.Username) > 64 {
		fail(w, 400, "Password needs 12–72 characters, uppercase, lowercase, number and symbol")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(v.Password), 12)
	if e != nil {
		fail(w, 500, "Password unavailable")
		return
	}
	account := Account{v.Username, string(hash)}
	tx, e := a.db.Begin()
	if e == nil {
		e = storeJSON(tx, "account", account)
		if e == nil {
			_, e = tx.Exec("DELETE FROM sessions")
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if e != nil {
		fail(w, 500, "Save failed")
		return
	}
	a.account = account
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func main() {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		dir = "data"
	}
	a, e := openApp(dir)
	if e != nil {
		log.Fatal("Cannot open data directory: ", e)
	}
	defer a.db.Close()
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	s := &http.Server{Addr: addr, Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.Shutdown(c)
		close(shutdownDone)
	}()
	log.Printf("Nest Panel %s listening on %s", version, addr)
	if e = s.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
	if ctx.Err() != nil {
		<-shutdownDone
	}
}
