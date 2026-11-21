package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"image"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Bundle struct {
	App              string            `json:"app"`
	Version          int               `json:"version"`
	Exported         string            `json:"exported"`
	Config           Config            `json:"config"`
	Images           map[string]string `json:"images"`
	Account          *Account          `json:"account,omitempty"`
	BackgroundSource string            `json:"backgroundSource,omitempty"`
	Warnings         []string          `json:"warnings,omitempty"`
}
type Sealed struct {
	App     string `json:"app"`
	Version int    `json:"version"`
	Salt    string `json:"salt"`
	Nonce   string `json:"nonce"`
	Data    string `json:"data"`
}

func (a *App) bundle(full bool) (Bundle, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	b := Bundle{App: "NestPanel", Version: 1, Exported: time.Now().UTC().Format(time.RFC3339), Config: a.config, Images: map[string]string{}, Warnings: []string{}}
	if full {
		account := a.account
		b.Account = &account
		b.BackgroundSource = a.backgroundSource
	}
	used := map[string]bool{}
	if b.Config.Theme.Background != "" {
		used[b.Config.Theme.Background] = true
	}
	for _, g := range b.Config.Groups {
		for _, s := range g.Sites {
			if s.Icon != "" {
				used[s.Icon] = true
			}
		}
	}
	for id := range used {
		var data []byte
		if e := a.db.QueryRow("SELECT data FROM icons WHERE id=?", id).Scan(&data); e != nil {
			return b, errors.New("Image missing; repair the website or background before exporting")
		}
		b.Images[id] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return b, nil
}
func attachment(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
}
func (a *App) exportConfig(w http.ResponseWriter, r *http.Request) {
	b, e := a.bundle(false)
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	attachment(w, "nest-panel-config.json")
	jsonOut(w, 200, b)
}
func parseImport(raw json.RawMessage) (Bundle, error) {
	var header struct {
		App, AppName string
		Version      int
	}
	if e := json.Unmarshal(raw, &header); e != nil {
		return Bundle{}, errors.New("Invalid JSON")
	}
	if header.App == "NestPanel" {
		var b Bundle
		if e := json.Unmarshal(raw, &b); e != nil {
			return b, errors.New("Invalid configuration")
		}
		if b.Version != 1 {
			return b, errors.New("Unsupported configuration version")
		}
		b.Account = nil
		b.BackgroundSource = ""
		return b, validateBundle(&b)
	}
	if header.AppName != "Sun-Panel-Config" || header.Version != 1 {
		return Bundle{}, errors.New("Not a supported Sun-Panel v1 JSON or NestPanel configuration / 不支持该导入格式")
	}
	var sun struct {
		Icons []struct {
			Title    string
			Sort     int
			Children []struct {
				Title, URL, LanURL, Description string
				Sort                            int
				CardType                        int
				Icon                            *struct {
					ItemType  int
					Src, Text string
				}
			}
		}
	}
	if e := json.Unmarshal(raw, &sun); e != nil {
		return Bundle{}, e
	}
	b := Bundle{App: "NestPanel", Version: 1, Config: defaultConfig(), Images: map[string]string{}, Warnings: []string{}}
	b.Config.Groups = []Group{}
	sort.SliceStable(sun.Icons, func(i, j int) bool { return sun.Icons[i].Sort < sun.Icons[j].Sort })
	skipped, missing := 0, 0
	for _, g := range sun.Icons {
		group := Group{ID: newID(), Title: g.Title, Sites: []Site{}}
		if group.Title == "" {
			group.Title = "导入分组"
		}
		sort.SliceStable(g.Children, func(i, j int) bool { return g.Children[i].Sort < g.Children[j].Sort })
		for _, s := range g.Children {
			rawURL := s.URL
			if rawURL == "" {
				rawURL = s.LanURL
			}
			u, e := validURL(rawURL)
			if e != nil {
				skipped++
				continue
			}
			title := s.Title
			if title == "" {
				title = u.Hostname()
			}
			site := Site{ID: newID(), Title: title, URL: u.String(), Description: s.Description}
			if s.Icon != nil && strings.HasPrefix(s.Icon.Src, "data:") {
				if data, e := decodeDataURL(s.Icon.Src, 1<<20); e == nil {
					if png, e := imageBytes(data, false); e == nil {
						h := sha256.Sum256(png)
						site.Icon = hex.EncodeToString(h[:])
						b.Images[site.Icon] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
					}
				}
			}
			if site.Icon == "" {
				missing++
			}
			group.Sites = append(group.Sites, site)
		}
		b.Config.Groups = append(b.Config.Groups, group)
	}
	if skipped > 0 {
		b.Warnings = append(b.Warnings, fmt.Sprintf("Skipped %d unsupported cards/URLs / 已跳过 %d 个非网站或无效链接", skipped, skipped))
	}
	if missing > 0 {
		b.Warnings = append(b.Warnings, fmt.Sprintf("%d icons need fetching or use the default icon / %d 个图标需重新获取或使用默认图标", missing, missing))
	}
	b.Warnings = append(b.Warnings, "One address per website: URL first, LAN URL only when URL is empty / 每个网站保留一个地址，优先 URL，空缺时使用 LAN 地址")
	return b, validateBundle(&b)
}
func validateBundle(b *Bundle) error {
	if b.App != "NestPanel" || b.Version != 1 {
		return errors.New("Unsupported backup format")
	}
	if b.BackgroundSource != "" {
		if _, e := validURL(b.BackgroundSource); e != nil {
			return errors.New("Invalid background API address")
		}
	}
	if e := validateConfig(b.Config); e != nil {
		return e
	}
	if len(b.Images) > 5001 {
		return errors.New("Too many images")
	}
	size := 0
	for id, s := range b.Images {
		limit := 256 << 10
		if id == b.Config.Theme.Background {
			limit = 8 << 20
		}
		data, e := decodeDataURL(s, limit)
		if e != nil {
			return e
		}
		cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
		side := 128
		if id == b.Config.Theme.Background {
			side = 1920
		}
		if e != nil || format != "png" || cfg.Width > side || cfg.Height > side || cfg.Width < 1 || cfg.Height < 1 {
			return errors.New("Invalid normalized backup image")
		}
		size += len(data)
		if size > 32<<20 {
			return errors.New("Images exceed 32 MB backup limit")
		}
		h := sha256.Sum256(data)
		if id != hex.EncodeToString(h[:]) {
			return errors.New("Image checksum mismatch")
		}
		if _, e := imageBytes(data, id == b.Config.Theme.Background); e != nil {
			return e
		}
	}
	used := map[string]bool{}
	if b.Config.Theme.Background != "" {
		used[b.Config.Theme.Background] = true
	}
	for _, g := range b.Config.Groups {
		for _, s := range g.Sites {
			if s.Icon != "" {
				used[s.Icon] = true
			}
		}
	}
	for id := range used {
		if _, ok := b.Images[id]; !ok {
			return errors.New("Backup has missing images")
		}
	}
	return nil
}
func (a *App) previewImport(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decode(w, r, &raw, 48<<20) {
		return
	}
	b, e := parseImport(raw)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	count := 0
	for _, g := range b.Config.Groups {
		count += len(g.Sites)
	}
	titles := []string{}
	for _, g := range b.Config.Groups {
		titles = append(titles, g.Title)
	}
	jsonOut(w, 200, map[string]any{"groups": titles, "sites": count, "images": len(b.Images), "warnings": b.Warnings})
}
func (a *App) importConfig(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Data     json.RawMessage `json:"data"`
		Mode     string          `json:"mode"`
		Revision int64           `json:"revision"`
	}
	if !decode(w, r, &v, 48<<20) {
		return
	}
	if !oneOf(v.Mode, "merge", "replace") {
		fail(w, 400, "Invalid import mode")
		return
	}
	b, e := parseImport(v.Data)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v.Revision != a.config.Revision {
		fail(w, 409, "Settings changed; reload before importing")
		return
	}
	next := b.Config
	next.Public = a.config.Public
	next.Revision = a.config.Revision + 1
	if v.Mode == "merge" {
		next = a.config
		next.Revision++
		seen := map[string]bool{}
		for _, g := range next.Groups {
			for _, s := range g.Sites {
				seen[canonicalURL(s.URL)] = true
			}
		}
		for _, g := range b.Config.Groups {
			idx := -1
			for i, existing := range next.Groups {
				if existing.Title == g.Title {
					idx = i
					break
				}
			}
			if idx < 0 {
				next.Groups = append(next.Groups, Group{ID: newID(), Title: g.Title, Sites: []Site{}})
				idx = len(next.Groups) - 1
			}
			for _, s := range g.Sites {
				key := canonicalURL(s.URL)
				if !seen[key] {
					s.ID = newID()
					next.Groups[idx].Sites = append(next.Groups[idx].Sites, s)
					seen[key] = true
				}
			}
		}
	}
	if e = validateConfig(next); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if e = a.commitBundle(next, b.Images, nil, false); e != nil {
		fail(w, 500, "Import failed; previous configuration retained")
		return
	}
	a.config = next
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) commitBundle(c Config, images map[string]string, account *Account, replace bool) error {
	return a.commitBundleWithSource(c, images, account, replace, a.backgroundSource)
}
func (a *App) commitBundleWithSource(c Config, images map[string]string, account *Account, replace bool, source string) error {
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if replace {
		if _, e = tx.Exec("DELETE FROM icons; DELETE FROM sessions"); e != nil {
			return e
		}
		if e = storeJSON(tx, "background_source", source); e != nil {
			return e
		}
	}
	if e = storeJSON(tx, "config", c); e != nil {
		return e
	}
	for id, s := range images {
		data, e := decodeDataURL(s, 8<<20)
		if e != nil {
			return e
		}
		if _, e = tx.Exec("INSERT OR IGNORE INTO icons(id,mime,data) VALUES(?,?,?)", id, "image/png", data); e != nil {
			return e
		}
	}
	if account != nil {
		if e = storeJSON(tx, "account", account); e != nil {
			return e
		}
	}
	var used int64
	if e = tx.QueryRow("SELECT COALESCE(SUM(length(data)),0) FROM icons").Scan(&used); e != nil {
		return e
	}
	if used > 128<<20 {
		return errors.New("Image storage limit exceeded")
	}
	if e = validateReferences(tx, c); e != nil {
		return e
	}
	return tx.Commit()
}
func validateReferences(tx *sql.Tx, c Config) error {
	ids := map[string]bool{}
	if c.Theme.Background != "" {
		ids[c.Theme.Background] = true
	}
	for _, g := range c.Groups {
		for _, s := range g.Sites {
			if s.Icon != "" {
				ids[s.Icon] = true
			}
		}
	}
	var total int64
	for id := range ids {
		var size int64
		if e := tx.QueryRow("SELECT length(data) FROM icons WHERE id=?", id).Scan(&size); e != nil {
			return errors.New("Referenced image is missing")
		}
		total += size
		if total > 32<<20 {
			return errors.New("Referenced images exceed the portable backup limit of 32 MB")
		}
	}
	return nil
}
func seal(data []byte, password string) (Sealed, error) {
	s := Sealed{App: "NestPanel-Encrypted", Version: 1}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, e := rand.Read(salt); e != nil {
		return s, e
	}
	if _, e := rand.Read(nonce); e != nil {
		return s, e
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 210000, 32)
	if e != nil {
		return s, e
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	s.Salt = base64.StdEncoding.EncodeToString(salt)
	s.Nonce = base64.StdEncoding.EncodeToString(nonce)
	s.Data = base64.StdEncoding.EncodeToString(gcm.Seal(nil, nonce, data, []byte("NestPanel-Encrypted:1")))
	return s, nil
}
func unseal(s Sealed, password string) ([]byte, error) {
	bad := errors.New("Wrong password or damaged backup / 备份密码不正确或文件损坏")
	if s.App != "NestPanel-Encrypted" || s.Version != 1 || len(password) < 12 || len(password) > 128 {
		return nil, bad
	}
	salt, e := base64.StdEncoding.DecodeString(s.Salt)
	if e != nil || len(salt) != 16 {
		return nil, bad
	}
	nonce, e := base64.StdEncoding.DecodeString(s.Nonce)
	if e != nil || len(nonce) != 12 {
		return nil, bad
	}
	data, e := base64.StdEncoding.DecodeString(s.Data)
	if e != nil || len(data) > 48<<20 {
		return nil, bad
	}
	key, e := pbkdf2.Key(sha256.New, password, salt, 210000, 32)
	if e != nil {
		return nil, bad
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	plain, e := gcm.Open(nil, nonce, data, []byte("NestPanel-Encrypted:1"))
	if e != nil {
		return nil, bad
	}
	return plain, nil
}
func (a *App) backup(w http.ResponseWriter, r *http.Request) {
	var v struct{ Password string }
	if !decode(w, r, &v, 4096) {
		return
	}
	if len(v.Password) < 12 || len(v.Password) > 128 {
		fail(w, 400, "Backup password must be 12–128 characters / 备份密码需12–128位")
		return
	}
	b, e := a.bundle(true)
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	raw, _ := json.Marshal(b)
	sealed, e := seal(raw, v.Password)
	if e != nil {
		fail(w, 500, "Cannot encrypt backup")
		return
	}
	attachment(w, "nest-panel-backup.nestbackup")
	jsonOut(w, 200, sealed)
}
func (a *App) restore(w http.ResponseWriter, r *http.Request) {
	var v struct {
		Password string
		Backup   Sealed
		Revision int64
	}
	if !decode(w, r, &v, 64<<20) {
		return
	}
	plain, e := unseal(v.Backup, v.Password)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	var b Bundle
	if json.Unmarshal(plain, &b) != nil {
		fail(w, 400, "Invalid backup")
		return
	}
	if e = validateBundle(&b); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if b.Account == nil || b.Account.Username == "" || len(b.Account.Username) > 64 {
		fail(w, 400, "Missing account in complete backup")
		return
	}
	if cost, e := bcrypt.Cost([]byte(b.Account.Hash)); e != nil || cost < 10 || cost > 14 {
		fail(w, 400, "Invalid account in backup")
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v.Revision != a.config.Revision {
		fail(w, 409, "Settings changed; reload before restoring")
		return
	}
	b.Config.Revision = a.config.Revision + 1
	if e = a.commitBundleWithSource(b.Config, b.Images, b.Account, true, b.BackgroundSource); e != nil {
		fail(w, 500, "Restore failed; previous data retained")
		return
	}
	a.config = b.Config
	a.account = *b.Account
	a.backgroundSource = b.BackgroundSource
	jsonOut(w, 200, map[string]bool{"ok": true})
}
