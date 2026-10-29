// NearTransfer owns a session-scoped HTTP server; all content stays on this Mac.
package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS

const maxBytes int64 = 1 << 30
const maxItems = 1000
const maxConcurrent = 4

type Item struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	MIME    string `json:"mime"`
	Origin  string `json:"origin"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}
type Store struct {
	mu      sync.Mutex
	dir     string
	items   []Item
	pending int
}
type Session struct {
	Token string   `json:"token"`
	Admin string   `json:"admin"`
	Local string   `json:"local"`
	URLs  []string `json:"urls"`
	QRDir string   `json:"-"`
}

func randomID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func validID(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func newStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, items: []Item{}}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err == nil {
		err = json.Unmarshal(b, &s.items)
	}
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if s.items == nil {
		return nil, errors.New("stored inbox index must be an array")
	}
	if len(s.items) > maxItems {
		return nil, errors.New("stored inbox exceeds capacity")
	}
	ids := map[string]bool{}
	for _, i := range s.items {
		if !validID(i.ID) || ids[i.ID] {
			return nil, errors.New("invalid stored item identity")
		}
		ids[i.ID] = true
		stat, e := os.Lstat(filepath.Join(dir, i.ID))
		if e != nil || !stat.Mode().IsRegular() || stat.Size() != i.Size {
			return nil, fmt.Errorf("stored file is missing or changed: %s", i.Name)
		}
	}
	// Interrupted uploads are not published. Remove only this store's reserved temp files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".upload-") && validID(strings.TrimPrefix(entry.Name(), ".upload-")) {
			if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}
func sameSecret(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }
func (s *Store) handler(session Session) http.Handler {
	allowed := map[string]bool{}
	for _, raw := range append(append([]string{}, session.URLs...), session.Local) {
		u, _ := url.Parse(raw)
		allowed[u.Host] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if !allowed[r.Host] {
			http.Error(w, "Unrecognized host", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "Cross-origin request blocked", 403)
			return
		}
		if r.Method == "GET" {
			asset := map[string]string{"/": "index.html", "/app.js": "app.js", "/style.css": "style.css"}[r.URL.Path]
			if asset != "" {
				b, _ := web.ReadFile("web/" + asset)
				w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(asset)))
				w.Write(b)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/download/") && r.Method == "POST" {
			s.download(w, r, session.Token)
			return
		}
		if !sameSecret(r.Header.Get("Authorization"), "Bearer "+session.Token) {
			http.Error(w, "配对已失效，请重新扫描 Mac 上的二维码", 401)
			return
		}
		if r.URL.Path == "/pairing" && r.Method == "GET" {
			if !sameSecret(r.Header.Get("X-Admin"), session.Admin) {
				http.Error(w, "Mac only", 403)
				return
			}
			peers := []map[string]string{}
			for i, u := range session.URLs {
				png, e := os.ReadFile(filepath.Join(session.QRDir, strconv.Itoa(i)+".png"))
				if e != nil {
					http.Error(w, "二维码尚未就绪，请重试", 503)
					return
				}
				peers = append(peers, map[string]string{"url": u + "/#token=" + session.Token, "qr": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)})
			}
			jsonResponse(w, peers)
			return
		}
		if r.URL.Path == "/items" && r.Method == "GET" {
			s.mu.Lock()
			defer s.mu.Unlock()
			jsonResponse(w, s.items)
			return
		}
		if r.URL.Path == "/items" && r.Method == "POST" {
			s.upload(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/items/") {
			id := strings.TrimPrefix(r.URL.Path, "/items/")
			s.mu.Lock()
			var found *Item
			for _, v := range s.items {
				if v.ID == id {
					c := v
					found = &c
					break
				}
			}
			s.mu.Unlock()
			if found == nil {
				http.NotFound(w, r)
				return
			}
			if r.Method == "GET" {
				w.Header().Set("Content-Type", found.MIME)
				http.ServeFile(w, r, filepath.Join(s.dir, found.ID))
				return
			}
		}
		http.Error(w, "Unknown operation", 404)
	})
}
func jsonResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func (s *Store) download(w http.ResponseWriter, r *http.Request, token string) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := r.ParseForm(); err != nil || !sameSecret(r.PostForm.Get("token"), token) {
		http.Error(w, "配对已失效，请重新扫码", 401)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/download/")
	s.mu.Lock()
	var item *Item
	for _, i := range s.items {
		if i.ID == id {
			v := i
			item = &v
			break
		}
	}
	s.mu.Unlock()
	if item == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, filepath.Join(s.dir, item.ID))
}

func (s *Store) upload(w http.ResponseWriter, r *http.Request) {
	name, err := url.PathUnescape(r.Header.Get("X-File-Name"))
	if err != nil || name == "" || len(name) > 1024 || strings.ContainsAny(name, "\x00\r\n") {
		http.Error(w, "文件名无效", 400)
		return
	}
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == ".." || name == "/" {
		http.Error(w, "文件名无效", 400)
		return
	}
	origin := r.Header.Get("X-Origin")
	if origin != "mac" && origin != "browser" {
		http.Error(w, "Invalid origin", 400)
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		contentType = "application/octet-stream"
	}
	if r.ContentLength > maxBytes {
		http.Error(w, "单个文件不能超过 1 GiB", 413)
		return
	}
	s.mu.Lock()
	if len(s.items)+s.pending >= maxItems {
		s.mu.Unlock()
		http.Error(w, "收件箱已满（1000 项），请在 Mac 上归档后重启", 507)
		return
	}
	if s.pending >= maxConcurrent {
		s.mu.Unlock()
		http.Error(w, "同时传输过多，请稍后重试", 429)
		return
	}
	s.pending++
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.pending--; s.mu.Unlock() }()
	id := randomID()
	temp := filepath.Join(s.dir, ".upload-"+id)
	path := filepath.Join(s.dir, id)
	f, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, "Mac 无法创建文件，请检查磁盘空间", 507)
		return
	}
	published := false
	defer func() {
		f.Close()
		if !published {
			os.Remove(temp)
		}
	}()
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, maxBytes))
	if err != nil {
		http.Error(w, "传输中断或文件超过 1 GiB", 413)
		return
	}
	if err = f.Sync(); err == nil {
		err = f.Close()
	}
	if err != nil {
		http.Error(w, "Mac 无法保存文件", 507)
		return
	}
	item := Item{id, name, contentType, origin, n, time.Now().UTC().Format(time.RFC3339)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = os.Rename(temp, path); err != nil {
		http.Error(w, "Mac 无法保存文件", 507)
		return
	}
	next := append(append([]Item{}, s.items...), item)
	b, err := json.Marshal(next)
	if err == nil {
		err = writeIndex(s.dir, b)
	}
	if err != nil {
		os.Remove(path)
		http.Error(w, "Mac 无法保存索引", 507)
		return
	}
	s.items = next
	published = true
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	json.NewEncoder(w).Encode(item)
}
func writeIndex(dir string, b []byte) error {
	f, err := os.OpenFile(filepath.Join(dir, "index.tmp"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(filepath.Join(dir, "index.tmp"), filepath.Join(dir, "index.json"))
}
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: neartransfer-engine STORAGE_DIRECTORY QR_DIRECTORY")
		os.Exit(1)
	}
	// An advisory lock prevents two app instances from changing the same inbox.
	lock, err := os.OpenFile(filepath.Join(os.Args[1]+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		fail(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fail(errors.New("另一个近传实例正在使用此收件箱"))
	}
	s, err := newStore(os.Args[1])
	if err != nil {
		fail(err)
	}
	ln, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		fail(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	hosts := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		fail(err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ip, _, e := net.ParseCIDR(a.String())
			if e == nil && ip.To4() != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
				hosts = append(hosts, fmt.Sprintf("http://%s:%d", ip, port))
			}
		}
	}
	session := Session{Token: randomID(), Admin: randomID(), Local: fmt.Sprintf("http://127.0.0.1:%d", port), URLs: hosts, QRDir: os.Args[2]}
	server := &http.Server{Handler: s.handler(session), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Minute, WriteTimeout: 30 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Parent owns stdin. EOF also stops the listener after an abrupt launcher exit.
	go func() { io.Copy(io.Discard, os.Stdin); stop() }()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		server.Shutdown(c)
		server.Close()
	}()
	json.NewEncoder(os.Stdout).Encode(session)
	if err = server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
