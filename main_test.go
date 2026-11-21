package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	a, e := openApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { a.db.Close() })
	return a
}
func request(t *testing.T, a *App, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Requested-With", "NestPanel")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	return w
}
func setupCookie(t *testing.T, a *App) *http.Cookie {
	t.Helper()
	w := request(t, a, "POST", "/api/setup", map[string]string{"username": "admin", "password": "Example!Pass456"}, nil)
	if w.Code != 200 {
		t.Fatalf("setup status %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if strings.HasPrefix(c.Name, "nest_session_") {
			return c
		}
	}
	t.Fatal("missing cookie")
	return nil
}
func status(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d, response=%s", w.Code, want, w.Body.String())
	}
}
func imageData(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	img.Set(5, 5, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if e := png.Encode(&b, img); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestAuthenticationAndPublicBoundary(t *testing.T) {
	a := testApp(t)
	status(t, request(t, a, "GET", "/api/panel", nil, nil), 401)
	c := setupCookie(t, a)
	status(t, request(t, a, "POST", "/api/setup", map[string]string{"username": "other", "password": "Example!Pass456"}, nil), 409)
	status(t, request(t, a, "GET", "/api/panel", nil, c), 200)
	status(t, request(t, a, "GET", "/api/export", nil, nil), 401)
	status(t, request(t, a, "GET", "/api/docker", nil, nil), 401)
	cfg := a.config
	cfg.Public = true
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	w := request(t, a, "GET", "/api/panel", nil, nil)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), "password") {
		t.Fatal("account leaked")
	}
	status(t, request(t, a, "PUT", "/api/panel", a.config, nil), 401)
	cfg = a.config
	cfg.Public = false
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	status(t, request(t, a, "GET", "/api/panel", nil, nil), 401)
	status(t, request(t, a, "POST", "/api/logout", map[string]string{}, c), 200)
	status(t, request(t, a, "GET", "/api/panel", nil, c), 401)
}
func TestCSRFConflictAndPasswordRevocation(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	r := httptest.NewRequest("PUT", "/api/panel", nil)
	r.AddCookie(c)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	status(t, w, 403)
	r = httptest.NewRequest("POST", "/api/logout", strings.NewReader("{}"))
	r.AddCookie(c)
	r.Header.Set("X-Requested-With", "NestPanel")
	r.Header.Set("Origin", "https://attacker.example")
	w = httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	status(t, w, 403)
	cfg := a.config
	cfg.Revision = 100
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 409)
	bad := a.config
	bad.Groups = []Group{{ID: "a", Title: "Unsafe", Sites: []Site{{ID: "b", Title: "Bad", URL: "javascript:alert(1)"}}}}
	status(t, request(t, a, "PUT", "/api/panel", bad, c), 400)
	status(t, request(t, a, "POST", "/api/password", map[string]string{"current": "Example!Pass456", "username": "changed", "password": "Changed!Pass789"}, c), 200)
	status(t, request(t, a, "GET", "/api/panel", nil, c), 401)
	status(t, request(t, a, "POST", "/api/login", map[string]string{"username": "admin", "password": "Example!Pass456"}, nil), 401)
	status(t, request(t, a, "POST", "/api/login", map[string]string{"username": "changed", "password": "Changed!Pass789"}, nil), 200)
}
func TestSessionExpiryPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	a, e := openApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := setupCookie(t, a)
	a.db.Close()
	a, e = openApp(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer a.db.Close()
	status(t, request(t, a, "GET", "/api/panel", nil, c), 200)
	a.db.Exec("UPDATE sessions SET expires=?", time.Now().Add(-time.Minute).Unix())
	status(t, request(t, a, "GET", "/api/panel", nil, c), 401)
}
func TestSunPanelImportMergeAndLimits(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	sun := json.RawMessage(`{"appName":"Sun-Panel-Config","version":1,"icons":[{"title":"Tools","sort":2,"children":[{"title":"B","url":"https://b.example","sort":2},{"title":"A","url":"https://a.example","lanUrl":"http://nas.local","sort":1},{"title":"LAN","lanUrl":"http://nas.local:8080"},{"title":"Unsafe","url":"javascript:alert(1)"}]}]}`)
	b, e := parseImport(sun)
	if e != nil {
		t.Fatal(e)
	}
	if len(b.Config.Groups) != 1 || len(b.Config.Groups[0].Sites) != 3 {
		t.Fatal("bad import shape")
	}
	if b.Config.Groups[0].Sites[1].URL != "https://a.example" {
		t.Fatal("URL priority/order changed")
	}
	status(t, request(t, a, "POST", "/api/import/preview", sun, c), 200)
	for range 2 {
		status(t, request(t, a, "POST", "/api/import", map[string]any{"data": sun, "mode": "merge", "revision": a.config.Revision}, c), 200)
	}
	count := 0
	for _, g := range a.config.Groups {
		count += len(g.Sites)
	}
	if count != 3 {
		t.Fatalf("duplicates: %d", count)
	}
	status(t, request(t, a, "POST", "/api/import", map[string]any{"data": map[string]any{"appName": "Sun-Panel-Config", "version": 999}, "mode": "replace", "revision": a.config.Revision}, c), 400)
	if len(a.config.Groups) != 2 {
		t.Fatal("invalid import modified state")
	}
}
func TestPortableImagesEncryptedRestoreAndRollback(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	id, e := a.putImage(imageData(t), false)
	if e != nil {
		t.Fatal(e)
	}
	cfg := a.config
	cfg.Groups[0].Sites = []Site{{ID: newID(), Title: "Example", URL: "https://example.com", Icon: id}}
	cfg.Theme.Background = id
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	status(t, request(t, a, "GET", "/api/icons/"+id, nil, nil), 404)
	status(t, request(t, a, "GET", "/api/icons/"+id, nil, c), 200)
	b, e := a.bundle(true)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(b.Images[id], "data:image/png;base64,") {
		t.Fatal("missing base64")
	}
	raw, _ := json.Marshal(b)
	sealed, e := seal(raw, "Backup-Example123")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = unseal(sealed, "Wrong-Example123"); e == nil {
		t.Fatal("accepted wrong password")
	}
	target := testApp(t)
	tc := setupCookie(t, target)
	before := target.config.Revision
	status(t, request(t, target, "POST", "/api/restore", map[string]any{"backup": sealed, "password": "Wrong-Example123", "revision": before}, tc), 400)
	if target.config.Revision != before {
		t.Fatal("failed restore modified data")
	}
	status(t, request(t, target, "POST", "/api/restore", map[string]any{"backup": sealed, "password": "Backup-Example123", "revision": before}, tc), 200)
	status(t, request(t, target, "GET", "/api/panel", nil, tc), 401)
	w := request(t, target, "POST", "/api/login", map[string]string{"username": "admin", "password": "Example!Pass456"}, nil)
	status(t, w, 200)
	got, e := target.bundle(true)
	if e != nil {
		t.Fatal(e)
	}
	if got.Images[id] != b.Images[id] || got.Config.Theme.Background != id || got.Config.Groups[0].Sites[0].Icon != id {
		t.Fatal("restore lost images/layout")
	}
	unsafe := b
	unsafe.Images = map[string]string{id: "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(`<svg onload="alert(1)"/>`))}
	if validateBundle(&unsafe) == nil {
		t.Fatal("accepted active image")
	}
}
func TestNetworkAndImageSafety(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "169.254.169.254", "::1", "fe80::1", "0.0.0.0"} {
		if safeAddress(net.ParseIP(s)) {
			t.Fatalf("unsafe IP accepted: %s", s)
		}
	}
	for _, s := range []string{"192.168.10.1", "8.8.8.8"} {
		if !safeAddress(net.ParseIP(s)) {
			t.Fatal("valid admin LAN/public fetch rejected")
		}
	}
	for _, s := range []string{"file:///etc/passwd", "http://user:pass@example.com", "javascript:alert(1)"} {
		if _, e := validURL(s); e == nil {
			t.Fatal("unsafe URL accepted")
		}
	}
	if _, e := imageBytes([]byte(`<svg><script>alert(1)</script></svg>`), false); e == nil {
		t.Fatal("SVG accepted")
	}
	if _, e := imageBytes(imageData(t), false); e != nil {
		t.Fatal(e)
	}
	if !strongPassword("Example!Pass456") || strongPassword("weak") || strongPassword(strings.Repeat("Aa!1", 30)) {
		t.Fatal("strength validation")
	}
}

func TestBackgroundSourceNeverLeaksAndSurvivesFullRestore(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	a.mu.Lock()
	a.backgroundSource = "https://gallery.example.com/api/random?token=example-only"
	tx, e := a.db.Begin()
	if e == nil {
		e = storeJSON(tx, "background_source", a.backgroundSource)
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	a.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	cfg := a.config
	id, err := a.putImage(imageData(t), true)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Theme.Background = id
	cfg.Public = true
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	for _, p := range []string{"/api/panel", "/api/export"} {
		w := request(t, a, "GET", p, nil, c)
		status(t, w, 200)
		if strings.Contains(w.Body.String(), "example-only") || strings.Contains(w.Body.String(), "backgroundSource") {
			t.Fatal("API source leaked")
		}
	}
	status(t, request(t, a, "GET", "/api/background", nil, nil), 401)
	w := request(t, a, "GET", "/api/background", nil, c)
	status(t, w, 200)
	if strings.Contains(w.Body.String(), "example-only") {
		t.Fatal("secret echoed")
	}
	b, e := a.bundle(true)
	if e != nil {
		t.Fatal(e)
	}
	if b.BackgroundSource != a.backgroundSource {
		t.Fatal("full backup missing source")
	}
	raw, _ := json.Marshal(b)
	sealed, e := seal(raw, "Backup-Example123")
	if e != nil {
		t.Fatal(e)
	}
	target := testApp(t)
	tc := setupCookie(t, target)
	status(t, request(t, target, "POST", "/api/restore", map[string]any{"backup": sealed, "password": "Backup-Example123", "revision": target.config.Revision}, tc), 200)
	if target.backgroundSource != a.backgroundSource {
		t.Fatal("restore lost source")
	}
	status(t, request(t, a, "POST", "/api/background", map[string]any{"clear": true, "revision": a.config.Revision}, c), 200)
	if a.backgroundSource != "" || a.config.Theme.Background != "" {
		t.Fatal("API source and visible wallpaper must clear together")
	}
}

func TestICOUncompressedBitmap(t *testing.T) {
	dib := make([]byte, 48)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], 1)
	binary.LittleEndian.PutUint32(dib[8:12], 2)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	dib[42] = 255
	pngBytes, e := decodeDIB(dib)
	if e != nil {
		t.Fatal(e)
	}
	im, _, e := image.Decode(bytes.NewReader(pngBytes))
	if e != nil {
		t.Fatal(e)
	}
	r, _, _, alpha := im.At(0, 0).RGBA()
	if r != 65535 || alpha != 65535 {
		t.Fatal("ICO color or alpha lost")
	}
	for _, invalid := range [][]byte{nil, make([]byte, 40), dib[:42]} {
		if _, e := decodeDIB(invalid); e == nil {
			t.Fatal("accepted invalid ICO")
		}
	}
}
func TestLoginThrottle(t *testing.T) {
	a := testApp(t)
	setupCookie(t, a)
	for range 10 {
		status(t, request(t, a, "POST", "/api/login", map[string]string{"username": "admin", "password": "bad"}, nil), 401)
	}
	status(t, request(t, a, "POST", "/api/login", map[string]string{"username": "admin", "password": "Example!Pass456"}, nil), 429)
}

func TestHiddenGroupsAreFilteredByServerAndIconAuthorization(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	icon, e := a.putImage(imageData(t), false)
	if e != nil {
		t.Fatal(e)
	}
	cfg := a.config
	cfg.Public = true
	cfg.Groups = append(cfg.Groups, Group{ID: "hidden-group", Title: "Private group", Sites: []Site{{ID: "private-site", Title: "Private service", URL: "https://private.example.com", Icon: icon}}})
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	status(t, request(t, a, "GET", "/api/icons/"+icon, nil, nil), 200)
	cfg = a.config
	cfg.Groups[1].Hidden = true
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	public := request(t, a, "GET", "/api/panel", nil, nil)
	status(t, public, 200)
	for _, value := range []string{"Private group", "private.example.com", "hidden-group", icon} {
		if strings.Contains(public.Body.String(), value) {
			t.Fatalf("hidden information leaked: %s", value)
		}
	}
	status(t, request(t, a, "GET", "/api/icons/"+icon, nil, nil), 404)
	status(t, request(t, a, "GET", "/api/icons/"+icon, nil, c), 200)
	admin := request(t, a, "GET", "/api/panel", nil, c)
	if !strings.Contains(admin.Body.String(), "Private group") {
		t.Fatal("admin lost private group")
	}
	r := httptest.NewRequest("GET", "/api/icons/"+icon, nil)
	r.Header.Set("If-None-Match", `"`+icon+`"`)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, r)
	status(t, w, 404)
	bundle, e := a.bundle(false)
	if e != nil {
		t.Fatal(e)
	}
	if !bundle.Config.Groups[1].Hidden {
		t.Fatal("export lost privacy flag")
	}
	bundle.Config.Groups = append([]Group(nil), bundle.Config.Groups...)
	bundle.Config.Groups[1].Hidden = false
	status(t, request(t, a, "POST", "/api/import", map[string]any{"data": bundle, "mode": "merge", "revision": a.config.Revision}, c), 200)
	if !a.config.Groups[1].Hidden {
		t.Fatal("merge relaxed existing group privacy")
	}
	oversized := Bundle{App: "NestPanel", Version: 1, Config: defaultConfig(), Images: map[string]string{}}
	oversized.Config.Groups = []Group{{ID: newID(), Title: a.config.Groups[0].Title, Hidden: true, Sites: []Site{}}}
	for i := 0; i < 199; i++ {
		oversized.Config.Groups = append(oversized.Config.Groups, Group{ID: newID(), Title: fmt.Sprintf("Import %d", i), Sites: []Site{}})
	}
	status(t, request(t, a, "POST", "/api/import", map[string]any{"data": oversized, "mode": "merge", "revision": a.config.Revision}, c), 400)
	if a.config.Groups[0].Hidden {
		t.Fatal("failed import mutated live privacy settings")
	}
	cfg = a.config
	cfg.Groups[0].Hidden = true
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	var view Config
	w = request(t, a, "GET", "/api/panel", nil, nil)
	status(t, w, 200)
	if json.Unmarshal(w.Body.Bytes(), &view) != nil || len(view.Groups) != 0 {
		t.Fatal("hidden-only public panel leaked groups")
	}
}

func TestAppearanceCompatibilityAndImportVisibility(t *testing.T) {
	a := testApp(t)
	c := setupCookie(t, a)
	cfg := a.config
	mask := 45
	clock := true
	cfg.Theme.Navigation = "left"
	cfg.Theme.Surface = "glass"
	cfg.Theme.IconSize = 48
	cfg.Theme.Mask = &mask
	cfg.Theme.ShowClock = &clock
	cfg.Theme.Icons = "color"
	status(t, request(t, a, "PUT", "/api/panel", cfg, c), 200)
	b, e := a.bundle(true)
	if e != nil {
		t.Fatal(e)
	}
	if b.Config.Theme.Navigation != "left" || *b.Config.Theme.ShowClock != true {
		t.Fatal("appearance backup lost fields")
	}
	invalid := a.config
	invalid.Theme.Navigation = "unknown"
	status(t, request(t, a, "PUT", "/api/panel", invalid, c), 400)
	b, e = parseImport(json.RawMessage(`{"appName":"Sun-Panel-Config","version":1,"icons":[{"title":"Private","publicVisitHide":true,"children":[{"title":"Example","url":"https://example.com"}]}]}`))
	if e != nil || !b.Config.Groups[0].Hidden {
		t.Fatal("Sun-Panel visibility was not preserved")
	}
}
