package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

func mp4ContainerFixture() []byte {
	box := func(name string, body []byte) []byte {
		out := make([]byte, len(body)+8)
		binary.BigEndian.PutUint32(out, uint32(len(out)))
		copy(out[4:], name)
		copy(out[8:], body)
		return out
	}
	ftyp := box("ftyp", []byte("isom\x00\x00\x00\x00isommp42"))
	moov := box("moov", box("mvhd", make([]byte, 16)))
	return append(append(ftyp, moov...), box("mdat", []byte{1, 2, 3, 4})...)
}

func multipartVideoRequest(content []byte, filename, mediaType string) *http.Request {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+filename+`"`)
	header.Set("Content-Type", mediaType)
	part, _ := writer.CreatePart(header)
	_, _ = part.Write(content)
	_ = writer.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/media/upload", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestVideoContainerValidation(t *testing.T) {
	good := mp4ContainerFixture()
	if err := validateMP4(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{nil, []byte("<html><script>alert(1)</script></html>"), good[:24], good[:len(good)-1], append(append([]byte{}, good...), 1)} {
		if err := validateMP4(bad); err == nil {
			t.Fatal("malformed or truncated MP4 accepted")
		}
	}
}

func TestVideoMultipartValidation(t *testing.T) {
	for _, mediaType := range []string{"video/mp4", "application/octet-stream"} {
		if _, err := readUploadedVideo(multipartVideoRequest(mp4ContainerFixture(), "movie.mp4", mediaType)); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []*http.Request{
		multipartVideoRequest(mp4ContainerFixture(), "movie.mov", "video/mp4"),
		multipartVideoRequest(mp4ContainerFixture(), "movie.mp4", "image/png"),
		multipartVideoRequest([]byte("not a video"), "movie.mp4", "video/mp4"),
		httptest.NewRequest(http.MethodPost, "/api/media/upload", strings.NewReader("{}")),
	} {
		if _, err := readUploadedVideo(r); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
	oversized := multipartVideoRequest(make([]byte, maxVideoBytes+1), "movie.mp4", "video/mp4")
	if _, err := readUploadedVideo(oversized); !errors.Is(err, errVideoTooLarge) {
		t.Fatalf("oversized video should produce explicit error: %v", err)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for i := 0; i < 2; i++ {
		p, _ := w.CreateFormFile("file", "movie.mp4")
		_, _ = p.Write(mp4ContainerFixture())
	}
	_ = w.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/media/upload", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if _, err := readUploadedVideo(r); err == nil {
		t.Fatal("multiple video files accepted")
	}
}

type mediaTestDB struct {
	active       bool
	passwordHash string
	content      []byte
	mime         string
	id           string
	stored       int
}

type mediaTestConnector struct{ state *mediaTestDB }
type mediaTestDriver struct{ state *mediaTestDB }

func (c mediaTestConnector) Connect(context.Context) (driver.Conn, error) { return c.state, nil }
func (c mediaTestConnector) Driver() driver.Driver                        { return mediaTestDriver{c.state} }
func (d mediaTestDriver) Open(string) (driver.Conn, error)                { return d.state, nil }
func (d *mediaTestDB) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (d *mediaTestDB) Close() error              { return nil }
func (d *mediaTestDB) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (d *mediaTestDB) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.HasPrefix(query, "SELECT COUNT(*) FROM auth_sessions"):
		return &mediaTestRows{columns: []string{"count"}, values: []driver.Value{int64(1)}}, nil
	case strings.HasPrefix(query, "SELECT active,role,username,password_hash FROM admins"):
		return &mediaTestRows{columns: []string{"active", "role", "username", "password_hash"}, values: []driver.Value{d.active, "OPERATOR", "media-editor", d.passwordHash}}, nil
	case query == "SELECT payload FROM configs WHERE config_key=?":
		return &mediaTestRows{columns: []string{"payload"}}, nil
	case strings.HasPrefix(query, "SELECT mime,content FROM media"):
		row := &mediaTestRows{columns: []string{"mime", "content"}}
		if d.id == args[0].Value {
			row.values = []driver.Value{d.mime, d.content}
		}
		return row, nil
	default:
		return nil, errors.New("unexpected media query")
	}
}
func (d *mediaTestDB) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, "INSERT INTO media") {
		d.id, d.mime, d.content = args[0].Value.(string), args[1].Value.(string), append([]byte{}, args[2].Value.([]byte)...)
		d.stored++
	} else if !strings.HasPrefix(query, "INSERT INTO audit_logs") {
		return nil, errors.New("unexpected media write")
	}
	return driver.RowsAffected(1), nil
}

type mediaTestRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (r *mediaTestRows) Columns() []string { return r.columns }
func (r *mediaTestRows) Close() error      { return nil }
func (r *mediaTestRows) Next(out []driver.Value) error {
	if r.read || r.values == nil {
		return io.EOF
	}
	r.read = true
	copy(out, r.values)
	return nil
}

func testMediaServer(t *testing.T) (*Server, *mediaTestDB, string) {
	t.Helper()
	t.Setenv("TOKEN_SECRET", strings.Repeat("t", 32))
	t.Setenv("PUBLIC_BASE_URL", "https://media.example.com")
	t.Setenv("ADMIN_ALLOWED_ORIGIN", "https://admin.example.com")
	state := &mediaTestDB{active: true, passwordHash: "media-test-password-hash"}
	db := sql.OpenDB(mediaTestConnector{state})
	t.Cleanup(func() { _ = db.Close() })
	token, err := adminToken(Admin{ID: "ADM_test", Username: "media-editor", Role: "EDITOR", PasswordHash: state.passwordHash})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{store: &Store{db: db}}, state, token
}

func TestVideoUploadAuthorizationAndCORS(t *testing.T) {
	s, state, token := testMediaServer(t)
	for _, authorized := range []bool{false, true} {
		r := multipartVideoRequest(mp4ContainerFixture(), "movie.mp4", "video/mp4")
		r.Header.Set("Origin", "https://admin.example.com")
		if authorized {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		expected := http.StatusUnauthorized
		if authorized {
			expected = http.StatusOK
		}
		if w.Code != expected || w.Header().Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
			t.Fatalf("unexpected upload response %d %s %s", w.Code, w.Header(), w.Body.String())
		}
		if authorized {
			var result struct {
				OK   bool `json:"ok"`
				Data struct {
					FileID string `json:"fileID"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.OK || result.Data.FileID != "https://media.example.com/media/"+state.id {
				t.Fatalf("upload response incompatible: %v %s", err, w.Body.String())
			}
		}
	}
	if state.stored != 1 || state.mime != "video/mp4" || !bytes.Equal(state.content, mp4ContainerFixture()) {
		t.Fatal("upload should persist exactly one authenticated MP4")
	}
	state.active = false
	r := multipartVideoRequest(mp4ContainerFixture(), "movie.mp4", "video/mp4")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || state.stored != 1 {
		t.Fatal("inactive admin uploaded video")
	}
	for _, origin := range []string{"https://admin.example.com", "https://other.example.com"} {
		r := httptest.NewRequest(http.MethodOptions, "/api/media/upload", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || (w.Header().Get("Access-Control-Allow-Origin") != "") != (origin == "https://admin.example.com") {
			t.Fatal("preflight did not respect configured origin")
		}
	}
}

func TestVideoUploadSizeAndBadContent(t *testing.T) {
	s, state, token := testMediaServer(t)
	r := multipartVideoRequest([]byte("invalid MP4"), "movie.mp4", "video/mp4")
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || state.stored != 0 {
		t.Fatal("invalid MP4 persisted")
	}
	r = multipartVideoRequest(mp4ContainerFixture(), "movie.mp4", "video/mp4")
	r.ContentLength = maxVideoRequestBytes + 1
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || state.stored != 0 {
		t.Fatal("declared oversized upload accepted")
	}
	// Unknown content length must still enforce the actual file-size limit.
	r = multipartVideoRequest(make([]byte, maxVideoBytes+1), "movie.mp4", "video/mp4")
	r.ContentLength = -1
	r.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || state.stored != 0 {
		t.Fatalf("chunked oversized upload accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestMediaRangeAndHEAD(t *testing.T) {
	s, state, _ := testMediaServer(t)
	state.id, state.mime, state.content = "VID_test", "video/mp4", []byte("0123456789")
	cases := []struct {
		method, rangeHeader        string
		status                     int
		body, length, contentRange string
	}{
		{http.MethodGet, "", 200, "0123456789", "10", ""},
		{http.MethodHead, "", 200, "", "10", ""},
		{http.MethodGet, "bytes=2-5", 206, "2345", "4", "bytes 2-5/10"},
		{http.MethodGet, "bytes=-3", 206, "789", "3", "bytes 7-9/10"},
		{http.MethodHead, "bytes=2-5", 206, "", "4", "bytes 2-5/10"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "/media/VID_test", nil)
		r.Header.Set("Range", tc.rangeHeader)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != tc.status || w.Body.String() != tc.body || w.Header().Get("Content-Length") != tc.length || w.Header().Get("Content-Range") != tc.contentRange || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Accept-Ranges") != "bytes" {
			t.Fatalf("range/HEAD incorrect for %s %s: %d %s %s", tc.method, tc.rangeHeader, w.Code, w.Header(), w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/media/VID_test", nil)
	r.Header.Set("Range", "bytes=99-")
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusRequestedRangeNotSatisfiable || w.Header().Get("Content-Range") != "bytes */10" {
		t.Fatal("unsatisfiable ranges should return 416")
	}
	r = httptest.NewRequest(http.MethodGet, "/media/VID_test", nil)
	r.Header.Set("If-None-Match", `"VID_test"`)
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatal("conditional media request should return 304")
	}
}
