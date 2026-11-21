package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

func safeAddress(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
func decodeDIB(data []byte) ([]byte, error) {
	bad := errors.New("unsupported ICO bitmap")
	if len(data) < 40 {
		return nil, bad
	}
	header := int(binary.LittleEndian.Uint32(data[:4]))
	w := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	height := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	bits := int(binary.LittleEndian.Uint16(data[14:16]))
	if header < 40 || header > len(data) || w < 1 || w > 256 || height < 2 || height > 512 || height%2 != 0 || (bits != 24 && bits != 32) || binary.LittleEndian.Uint32(data[16:20]) != 0 {
		return nil, bad
	}
	h := height / 2
	stride := ((w*bits + 31) / 32) * 4
	maskStride := ((w + 31) / 32) * 4
	if len(data)-header < stride*h {
		return nil, bad
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hasAlpha := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := header + (h-1-y)*stride + x*bits/8
			alpha := uint8(255)
			if bits == 32 {
				alpha = data[p+3]
				if alpha > 0 {
					hasAlpha = true
				}
			}
			img.SetNRGBA(x, y, color.NRGBA{R: data[p+2], G: data[p+1], B: data[p], A: alpha})
		}
	}
	maskOffset := header + stride*h
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.PixOffset(x, y)
			if bits == 32 && !hasAlpha {
				img.Pix[p+3] = 255
			}
			m := maskOffset + (h-1-y)*maskStride + x/8
			if m < len(data) && (data[m]&(1<<uint(7-x%8))) != 0 {
				img.Pix[p+3] = 0
			}
		}
	}
	var out bytes.Buffer
	if e := png.Encode(&out, img); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
func newFetchClient() *http.Client {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10, TLSHandshakeTimeout: 4 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if !safeAddress(ip) {
				return nil, errors.New("blocked local or metadata address")
			}
		}
		for _, ip := range ips {
			c, e := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if e == nil {
				return c, nil
			}
		}
		return nil, errors.New("cannot connect")
	}}
	return &http.Client{Transport: tr, Timeout: 10 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		_, e := validURL(r.URL.String())
		return e
	}}
}
func fetchBytes(ctx context.Context, raw string, limit int64, prefix ...bool) ([]byte, *url.URL, error) {
	if _, e := validURL(raw); e != nil {
		return nil, nil, e
	}
	r, e := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if e != nil {
		return nil, nil, e
	}
	r.Header.Set("User-Agent", "NestPanel/"+version+" (+website metadata)")
	r.Header.Set("Accept", "text/html,image/*;q=0.9,*/*;q=0.5")
	resp, e := newFetchClient().Do(r)
	if e != nil {
		return nil, nil, errors.New("Unable to reach website / 无法访问网站")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, errors.New("Website rejected request / 网站拒绝请求")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil || (int64(len(b)) > limit && (len(prefix) == 0 || !prefix[0])) {
		return nil, nil, errors.New("Response too large / 响应过大")
	}
	if int64(len(b)) > limit {
		b = b[:limit]
	}
	return b, resp.Request.URL, nil
}
func imageBytes(data []byte, background bool) ([]byte, error) {
	// Accept PNG-backed ICO and bounded uncompressed 24/32-bit DIB entries.
	if len(data) > 6 && bytes.Equal(data[:4], []byte{0, 0, 1, 0}) {
		n := int(binary.LittleEndian.Uint16(data[4:6]))
		for i := 0; i < n && 6+(i+1)*16 <= len(data); i++ {
			p := data[6+i*16:]
			size := int(binary.LittleEndian.Uint32(p[8:12]))
			off := int(binary.LittleEndian.Uint32(p[12:16]))
			if off >= 0 && size > 0 && off <= len(data) && size <= len(data)-off {
				entry := data[off : off+size]
				if bytes.HasPrefix(entry, []byte{137, 80, 78, 71}) {
					data = entry
					break
				}
				if converted, e := decodeDIB(entry); e == nil {
					data = converted
					break
				}
			}
		}
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
	maxPixels := int64(4_000_000)
	if background {
		maxPixels = 12_000_000
	}
	if e != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, errors.New("Unsupported or oversized image / 图片格式不支持或尺寸过大")
	}
	src, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, errors.New("Invalid image")
	}
	maxSide := 128
	if background {
		maxSide = 1920
	}
	width, height := cfg.Width, cfg.Height
	if width > maxSide || height > maxSide {
		if width >= height {
			height = height * maxSide / width
			width = maxSide
		} else {
			width = width * maxSide / height
			height = maxSide
		}
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	b := src.Bounds()
	if width == cfg.Width && height == cfg.Height {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	} else {
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	}
	var out bytes.Buffer
	e = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, dst)
	if e != nil {
		return nil, e
	}
	limit := 256 << 10
	if background {
		limit = 8 << 20
	}
	if out.Len() > limit {
		return nil, errors.New("Image too large after compression / 压缩后图片仍然过大")
	}
	return out.Bytes(), nil
}
func (a *App) putImage(data []byte, background bool) (string, error) {
	b, e := imageBytes(data, background)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	id := hex.EncodeToString(h[:])
	var used int64
	if e = a.db.QueryRow("SELECT COALESCE(SUM(length(data)),0) FROM icons").Scan(&used); e != nil {
		return "", e
	}
	if used+int64(len(b)) > 128<<20 {
		return "", errors.New("Image storage limit reached (128 MB); export and restore to remove unused images")
	}
	_, e = a.db.Exec("INSERT OR IGNORE INTO icons(id,mime,data) VALUES(?,?,?)", id, "image/png", b)
	return id, e
}
func (a *App) metadata(w http.ResponseWriter, r *http.Request) {
	var v struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &v, 4096) {
		return
	}
	u, e := validURL(v.URL)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	select {
	case a.fetchSlots <- struct{}{}:
		defer func() { <-a.fetchSlots }()
	default:
		fail(w, 429, "Metadata busy, retry shortly")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 16*time.Second)
	defer cancel()
	title := u.Hostname()
	iconID := ""
	warnings := []string{}
	candidates := []string{}
	b, final, e := fetchBytes(ctx, u.String(), 512<<10, true)
	if e == nil {
		reader, _ := charset.NewReader(bytes.NewReader(b), "")
		doc, err := html.Parse(reader)
		if err == nil {
			var pageTitle, ogTitle string
			var walk func(*html.Node)
			walk = func(n *html.Node) {
				if n.Type == html.ElementNode {
					attrs := map[string]string{}
					for _, a := range n.Attr {
						attrs[strings.ToLower(a.Key)] = a.Val
					}
					if n.Data == "title" && n.FirstChild != nil {
						pageTitle = n.FirstChild.Data
					}
					if n.Data == "meta" && attrs["property"] == "og:title" {
						ogTitle = attrs["content"]
					}
					if n.Data == "link" && (strings.Contains(strings.ToLower(attrs["rel"]), "icon")) {
						if ref, e := url.Parse(attrs["href"]); e == nil {
							candidates = append(candidates, final.ResolveReference(ref).String())
						}
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(doc)
			if ogTitle != "" {
				title = ogTitle
			} else if pageTitle != "" {
				title = pageTitle
			}
		}
	} else {
		warnings = append(warnings, "Title unavailable / 未能获取标题，可手动填写")
	}
	u.Path = "/favicon.ico"
	u.RawQuery = ""
	u.Fragment = ""
	candidates = append(candidates, u.String())
	for i, raw := range candidates {
		if i >= 4 || ctx.Err() != nil {
			break
		}
		if strings.HasPrefix(raw, "data:") {
			if data, err := decodeDataURL(raw, 256<<10); err == nil {
				iconID, _ = a.putImage(data, false)
			}
		} else if b, _, err := fetchBytes(ctx, raw, 1<<20); err == nil {
			iconID, _ = a.putImage(b, false)
		}
		if iconID != "" {
			break
		}
	}
	if iconID == "" {
		warnings = append(warnings, "Using default icon / 使用默认图标")
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) > 100 {
		title = string([]rune(title)[:100])
	}
	jsonOut(w, 200, map[string]any{"title": title, "icon": iconID, "warnings": warnings})
}
func decodeDataURL(s string, limit int) ([]byte, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "data:image/") || !strings.HasSuffix(parts[0], ";base64") || len(parts[1]) > (limit*4/3+8) {
		return nil, errors.New("Invalid image data")
	}
	b, e := base64.StdEncoding.DecodeString(parts[1])
	if e != nil || len(b) > limit {
		return nil, errors.New("Invalid image data")
	}
	return b, nil
}
func (a *App) uploadImage(w http.ResponseWriter, r *http.Request) {
	select {
	case a.imageSlots <- struct{}{}:
		defer func() { <-a.imageSlots }()
	default:
		fail(w, 429, "Image processing busy, retry shortly")
		return
	}
	var v struct {
		Data       string `json:"data"`
		Background bool   `json:"background"`
	}
	if !decode(w, r, &v, 12<<20) {
		return
	}
	limit := 1 << 20
	if v.Background {
		limit = 8 << 20
	}
	b, e := decodeDataURL(v.Data, limit)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	id, e := a.putImage(b, v.Background)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	jsonOut(w, 200, map[string]string{"id": id})
}
func (a *App) icon(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validHash(id) {
		http.NotFound(w, r)
		return
	}
	auth := a.authenticated(r)
	a.mu.RLock()
	allowed := auth || a.config.Public
	referenced := id == a.config.Theme.Background
	for _, g := range a.config.Groups {
		if !auth && g.Hidden {
			continue
		}
		for _, s := range g.Sites {
			if s.Icon == id {
				referenced = true
			}
		}
	}
	a.mu.RUnlock()
	if !allowed || (!auth && !referenced) {
		http.NotFound(w, r)
		return
	}
	var data []byte
	var mime string
	if a.db.QueryRow("SELECT mime,data FROM icons WHERE id=?", id).Scan(&mime, &data) != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private,no-cache")
	w.Header().Set("ETag", `"`+id+`"`)
	if r.Header.Get("If-None-Match") == `"`+id+`"` {
		w.WriteHeader(304)
		return
	}
	w.Write(data)
}

func (a *App) backgroundStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	source := a.backgroundSource
	a.mu.RUnlock()
	host := ""
	if u, e := validURL(source); e == nil {
		host = u.Hostname()
	}
	jsonOut(w, 200, map[string]any{"configured": source != "", "host": host})
}
func (a *App) updateBackground(w http.ResponseWriter, r *http.Request) {
	var v struct {
		URL      string `json:"url"`
		Clear    bool   `json:"clear"`
		Revision int64  `json:"revision"`
	}
	if !decode(w, r, &v, 4096) {
		return
	}
	a.mu.RLock()
	source := a.backgroundSource
	a.mu.RUnlock()
	if v.URL != "" {
		source = strings.TrimSpace(v.URL)
	}
	id := ""
	if !v.Clear {
		if _, e := validURL(source); e != nil {
			fail(w, 400, "Enter an HTTP(S) API that directly returns an image / 请填写直接返回图片的HTTP(S)接口")
			return
		}
		select {
		case a.imageSlots <- struct{}{}:
			defer func() { <-a.imageSlots }()
		default:
			fail(w, 429, "Image processing busy, retry shortly")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 16*time.Second)
		defer cancel()
		b, _, e := fetchBytes(ctx, source, 8<<20)
		if e != nil {
			fail(w, 400, "Cannot fetch background; previous image retained / 无法获取背景，已保留原图片")
			return
		}
		id, e = a.putImage(b, true)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
	} else {
		source = ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if v.Revision != a.config.Revision {
		fail(w, 409, "Settings changed; reload before applying background")
		return
	}
	next := a.config
	next.Revision++
	// An empty ID clears the active background together with the API source.
	next.Theme.Background = id
	tx, e := a.db.Begin()
	if e == nil {
		defer tx.Rollback()
		e = validateReferences(tx, next)
		if e == nil {
			e = storeJSON(tx, "config", next)
		}
		if e == nil {
			e = storeJSON(tx, "background_source", source)
		}
		if e == nil {
			e = tx.Commit()
		}
	}
	if e != nil {
		fail(w, 500, "Cannot save background; previous configuration retained")
		return
	}
	a.config = next
	a.backgroundSource = source
	jsonOut(w, 200, next)
}

type DockerCandidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	URL      string `json:"url"`
	State    string `json:"state"`
	Existing bool   `json:"existing"`
}

func (a *App) docker(w http.ResponseWriter, r *http.Request) {
	socket := os.Getenv("DOCKER_SOCKET")
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}, Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://docker/containers/json?all=0", nil)
	resp, e := client.Do(req)
	if e != nil {
		fail(w, 503, "Docker discovery unavailable. Mount docker.sock to enable / 挂载 Docker Socket 后可启用自动发现")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fail(w, 502, "Docker API unavailable")
		return
	}
	var rows []struct {
		Names  []string
		State  string
		Labels map[string]string
		Ports  []struct {
			IP                      string
			PrivatePort, PublicPort int
			Type                    string
		}
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rows); e != nil {
		fail(w, 502, "Invalid Docker response")
		return
	}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = strings.Split(r.Host, ":")[0]
	}
	if strings.ContainsAny(host, "/@?# ") {
		fail(w, 400, "Enter only a hostname or IP")
		return
	}
	host = strings.Trim(host, "[]")
	a.mu.RLock()
	known := map[string]bool{}
	for _, g := range a.config.Groups {
		for _, s := range g.Sites {
			known[s.URL] = true
		}
	}
	a.mu.RUnlock()
	out := []DockerCandidate{}
	for _, row := range rows {
		if len(row.Names) == 0 {
			continue
		}
		name := strings.TrimPrefix(row.Names[0], "/")
		if row.Labels["nest-panel.hide"] == "true" {
			continue
		}
		for _, p := range row.Ports {
			if p.Type != "tcp" || p.PublicPort == 0 {
				continue
			}
			u := "http://" + net.JoinHostPort(host, fmt.Sprint(p.PublicPort))
			if custom := row.Labels["nest-panel.url"]; custom != "" {
				if _, e := validURL(custom); e == nil {
					u = custom
				}
			}
			title := row.Labels["nest-panel.title"]
			if title == "" {
				title = name
			}
			out = append(out, DockerCandidate{ID: name + ":" + fmt.Sprint(p.PrivatePort), Name: title, URL: u, State: row.State, Existing: known[u]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	jsonOut(w, 200, out)
}

// The importer only uses HTTP(S) links and embedded image data; never fetches untrusted import URLs.
func canonicalURL(s string) string {
	u, e := validURL(s)
	if e != nil {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String()
}
