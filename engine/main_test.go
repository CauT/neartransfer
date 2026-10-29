package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T) (*Store, http.Handler, Session) {
	t.Helper()
	s, e := newStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	session := Session{Token: randomID(), Admin: randomID(), Local: "http://localhost", QRDir: t.TempDir()}
	return s, s.handler(session), session
}
func request(h http.Handler, session Session, method, path string, body io.Reader, extra map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, body)
	r.Header.Set("Authorization", "Bearer "+session.Token)
	for k, v := range extra {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func send(t *testing.T, h http.Handler, session Session, name, origin, mime string, b []byte) Item {
	t.Helper()
	w := request(h, session, "POST", "/items", bytes.NewReader(b), map[string]string{"X-File-Name": url.PathEscape(name), "X-Origin": origin, "Content-Type": mime})
	if w.Code != 201 {
		t.Fatalf("send %d %s", w.Code, w.Body)
	}
	var i Item
	if e := json.Unmarshal(w.Body.Bytes(), &i); e != nil {
		t.Fatal(e)
	}
	return i
}
func TestBidirectionalRoundTripAndPersistence(t *testing.T) {
	s, h, session := fixture(t)
	cases := []struct {
		name, origin, mime string
		data               []byte
	}{{"你好.txt", "mac", "text/plain", []byte("你好，手机！\nhttps://example.org/?x=1&y=2")}, {"照片.png", "browser", "image/png", []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 255}}, {"../同名.bin", "browser", "application/octet-stream", bytes.Repeat([]byte{0, 1, 255, 47}, 65536)}}
	for _, c := range cases {
		item := send(t, h, session, c.name, c.origin, c.mime, c.data)
		w := request(h, session, "GET", "/items/"+item.ID, nil, nil)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), c.data) {
			t.Fatal("round trip mismatch")
		}
		if strings.Contains(item.Name, "/") {
			t.Fatal("unsafe display name")
		}
		w = request(h, session, "POST", "/download/"+item.ID, strings.NewReader(url.Values{"token": {session.Token}}.Encode()), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), c.data) || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("download mismatch")
		}
		if request(h, session, "POST", "/download/"+item.ID, strings.NewReader("token=wrong"), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}).Code != 401 {
			t.Fatal("unauthorized download")
		}

	}
	restored, e := newStore(s.dir)
	if e != nil || len(restored.items) != 3 {
		t.Fatalf("restore %v", e)
	}
	w := request(restored.handler(session), session, "GET", "/items", nil, nil)
	var items []Item
	json.Unmarshal(w.Body.Bytes(), &items)
	if len(items) != 3 || items[0].Origin != "mac" || items[1].Origin != "browser" {
		t.Fatal("missing shared history")
	}
}
func TestAuthHostOriginAndStatic(t *testing.T) {
	_, h, session := fixture(t)
	for _, path := range []string{"/items", "/pairing", "/items/anything"} {
		if request(h, Session{}, "GET", path, nil, nil).Code != 401 {
			t.Fatal(path)
		}
	}
	if request(h, session, "GET", "/pairing", nil, nil).Code != 403 {
		t.Fatal("peer accessed admin")
	}
	if request(h, session, "GET", "/items", nil, map[string]string{"Origin": "http://evil.example"}).Code != 403 {
		t.Fatal("cross origin")
	}
	r := httptest.NewRequest("GET", "http://evil.example/items", nil)
	r.Header.Set("Authorization", "Bearer "+session.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("host validation")
	}
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w = request(h, Session{}, "GET", path, nil, nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal("static", path)
		}
		if strings.Contains(w.Body.String(), session.Token) {
			t.Fatal("secret in public assets")
		}
	}
}

type brokenReader struct{ sent bool }

func (b *brokenReader) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, []byte("partial")), nil
	}
	return 0, errors.New("client disconnected")
}
func TestInterruptedAndInvalidUploadsDoNotPublish(t *testing.T) {
	s, h, session := fixture(t)
	head := map[string]string{"X-File-Name": "file.bin", "X-Origin": "browser"}
	w := request(h, session, "POST", "/items", &brokenReader{}, head)
	if w.Code != 413 || len(s.items) != 0 || s.pending != 0 {
		t.Fatal("interruption not rolled back")
	}
	files, _ := os.ReadDir(s.dir)
	if len(files) != 0 {
		t.Fatal("partial file leaked")
	}
	for _, name := range []string{"", "%00", "%ZZ", "..", "%2F", "%0D%0A"} {
		head["X-File-Name"] = name
		if request(h, session, "POST", "/items", strings.NewReader("x"), head).Code != 400 {
			t.Fatal("invalid accepted", name)
		}
	}
	r := httptest.NewRequest("POST", "http://localhost/items", strings.NewReader("x"))
	r.ContentLength = maxBytes + 1
	r.Header.Set("Authorization", "Bearer "+session.Token)
	r.Header.Set("X-File-Name", "big")
	r.Header.Set("X-Origin", "mac")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatal("size limit")
	}
}
func TestConcurrentSameNamesStayDistinct(t *testing.T) {
	s, h, session := fixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); send(t, h, session, "same.txt", "mac", "text/plain", []byte("same")) }()
	}
	wg.Wait()
	if len(s.items) != 4 {
		t.Fatal("lost uploads")
	}
	ids := map[string]bool{}
	for _, i := range s.items {
		if ids[i.ID] {
			t.Fatal("overwritten")
		}
		ids[i.ID] = true
	}
	restored, e := newStore(s.dir)
	if e != nil || len(restored.items) != 4 {
		t.Fatal(e)
	}
}
func TestCapacity(t *testing.T) {
	s, h, session := fixture(t)
	s.pending = maxConcurrent
	if request(h, session, "POST", "/items", strings.NewReader("x"), map[string]string{"X-File-Name": "x", "X-Origin": "mac"}).Code != 429 {
		t.Fatal("concurrency")
	}
	s.pending = 0
	s.items = make([]Item, maxItems)
	if request(h, session, "POST", "/items", strings.NewReader("x"), map[string]string{"X-File-Name": "x", "X-Origin": "mac"}).Code != 507 {
		t.Fatal("capacity")
	}
}
func TestRestoreRejectsDamagedStoreAndCleansPartial(t *testing.T) {
	s, h, session := fixture(t)
	id := randomID()
	temp := filepath.Join(s.dir, ".upload-"+id)
	os.WriteFile(temp, []byte("unfinished"), 0600)
	if _, e := newStore(s.dir); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(temp); !os.IsNotExist(e) {
		t.Fatal("partial retained")
	}
	item := send(t, h, session, "x", "mac", "text/plain", []byte("x"))
	os.WriteFile(filepath.Join(s.dir, item.ID), []byte("changed"), 0600)
	if _, e := newStore(s.dir); e == nil {
		t.Fatal("corrupt file accepted")
	}
}
func TestRealHTTPClientsShareInbox(t *testing.T) {
	s, _, session := fixture(t)
	srv := httptest.NewUnstartedServer(nil)
	session.Local = "http://" + srv.Listener.Addr().String()
	srv.Config.Handler = s.handler(session)
	srv.Start()
	defer srv.Close()
	client := srv.Client()
	for _, origin := range []string{"mac", "browser"} {
		body := []byte("真实 HTTP " + origin)
		r, _ := http.NewRequest("POST", srv.URL+"/items", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+session.Token)
		r.Header.Set("X-File-Name", url.PathEscape("文字.txt"))
		r.Header.Set("X-Origin", origin)
		r.Header.Set("Content-Type", "text/plain")
		res, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		var item Item
		json.NewDecoder(res.Body).Decode(&item)
		res.Body.Close()
		if res.StatusCode != 201 {
			t.Fatal(res.Status)
		}
		r, _ = http.NewRequest("GET", srv.URL+"/items/"+item.ID, nil)
		r.Header.Set("Authorization", "Bearer "+session.Token)
		res, e = client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if !bytes.Equal(body, got) {
			t.Fatal("wire mismatch")
		}
	}
}

func TestNullIndexFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.json"), []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newStore(dir); err == nil {
		t.Fatal("null index accepted")
	}
}
