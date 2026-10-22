package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxVideoBytes int64 = 20 << 20
const maxVideoRequestBytes int64 = maxVideoBytes + (64 << 10)

func adminCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Vary", "Origin")
	origin := os.Getenv("ADMIN_ALLOWED_ORIGIN")
	if origin != "" && r.Header.Get("Origin") == origin {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
}

func adminPreflight(w http.ResponseWriter, r *http.Request) {
	adminCORS(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uploadVideo(w http.ResponseWriter, r *http.Request) {
	adminCORS(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	// A context timeout alone does not interrupt reads from the request body.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(60 * time.Second))
	actor, err := s.store.requireAdmin(r)
	if err != nil {
		fail(w, http.StatusUnauthorized, err)
		return
	}
	if err = s.store.authorizeAdmin(r, actor, "admin.media.upload"); err != nil {
		if !writeAccessError(w, err) {
			fail(w, http.StatusServiceUnavailable, err)
		}
		return
	}
	base, err := validateContentURL(strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"), false)
	baseURL, parseErr := url.Parse(base)
	if parseErr != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		err = errors.New("PUBLIC_BASE_URL 不能包含 query 或 fragment")
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, errors.New("PUBLIC_BASE_URL 未配置为可播放的 HTTPS 地址"))
		return
	}
	if r.ContentLength > maxVideoRequestBytes {
		fail(w, http.StatusRequestEntityTooLarge, errors.New("视频文件不得超过 20MB"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxVideoRequestBytes)
	content, err := readUploadedVideo(r)
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		var timeout net.Error
		if errors.Is(err, errVideoTooLarge) || errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
			err = errVideoTooLarge
		} else if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
			status = http.StatusRequestTimeout
			err = errors.New("视频上传超时，请重试")
		}
		fail(w, status, err)
		return
	}
	id := randomID("VID")
	if _, err = s.store.db.ExecContext(ctx, "INSERT INTO media(id,mime,content) VALUES(?,?,?)", id, "video/mp4", content); err != nil {
		log.Printf("video upload storage error: %v", err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail(w, http.StatusRequestTimeout, errors.New("视频上传超时，请重试"))
		} else {
			fail(w, http.StatusInternalServerError, errors.New("视频保存失败，请重试"))
		}
		return
	}
	s.store.audit(ctx, actor, "MEDIA_UPLOAD", id, M{"bytes": len(content), "mime": "video/mp4"})
	writeJSON(w, http.StatusOK, M{"ok": true, "data": M{"fileID": base + "/media/" + id}, "requestId": randomID("REQ")})
}

var errVideoTooLarge = errors.New("视频文件不得超过 20MB")

func readUploadedVideo(r *http.Request) ([]byte, error) {
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, errors.New("请使用 multipart/form-data 上传视频")
	}
	var content []byte
	found := false
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if found || part.FormName() != "file" || part.FileName() == "" {
			return nil, errors.New("请仅上传一个名为 file 的 MP4 视频文件")
		}
		found = true
		if err = validateVideoPart(part); err != nil {
			return nil, err
		}
		content, err = io.ReadAll(io.LimitReader(part, maxVideoBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(content)) > maxVideoBytes {
			return nil, errVideoTooLarge
		}
		if err = validateMP4(content); err != nil {
			return nil, err
		}
		if err = part.Close(); err != nil {
			return nil, err
		}
	}
	if !found {
		return nil, errors.New("请选择 MP4 视频文件")
	}
	return content, nil
}

func validateVideoPart(part *multipart.Part) error {
	if strings.ToLower(filepath.Ext(part.FileName())) != ".mp4" {
		return errors.New("仅支持 MP4 视频，请先转码为 MP4")
	}
	mediaType, _, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
	// Some upload clients default to octet-stream; the MP4 bytes are still
	// checked independently rather than trusting this client-supplied header.
	if err != nil || mediaType != "video/mp4" && mediaType != "application/octet-stream" {
		return errors.New("视频类型无效，仅支持 video/mp4")
	}
	return nil
}

func validateMP4(content []byte) error {
	if len(content) < 24 || http.DetectContentType(content) != "video/mp4" {
		return errors.New("视频文件不是有效 MP4，请检查文件或重新转码")
	}
	// Check top-level boxes and require movie metadata and media payload. This
	// rejects truncated containers while the browser checks the codec/decoding.
	metadata, payload := false, false
	for offset := 0; offset < len(content); {
		if len(content)-offset < 8 {
			return errors.New("MP4 文件内容不完整")
		}
		size := uint64(binary.BigEndian.Uint32(content[offset : offset+4]))
		header := uint64(8)
		box := string(content[offset+4 : offset+8])
		if size == 1 {
			if len(content)-offset < 16 {
				return errors.New("MP4 文件内容不完整")
			}
			size = binary.BigEndian.Uint64(content[offset+8 : offset+16])
			header = 16
		} else if size == 0 {
			size = uint64(len(content) - offset)
		}
		if size < header || size > uint64(len(content)-offset) {
			return errors.New("MP4 文件内容不完整")
		}
		if box == "moov" && size > header {
			metadata = true
		}
		if box == "mdat" && size > header {
			payload = true
		}
		offset += int(size)
	}
	if !metadata || !payload {
		return errors.New("MP4 缺少视频元数据或内容，请重新转码")
	}
	return nil
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/media/")
	if !articleIDRE.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var mime string
	var content []byte
	if err := s.store.db.QueryRowContext(ctx, "SELECT mime,content FROM media WHERE id=?", id).Scan(&mime, &content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			fail(w, http.StatusServiceUnavailable, errors.New("媒体读取失败，请重试"))
		}
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+id+`"`)
	// ServeContent implements GET/HEAD, byte ranges (206/416), Content-Length,
	// and conditional requests so a player can seek without downloading again.
	http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(content))
}
