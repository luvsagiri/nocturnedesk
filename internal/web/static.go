package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	stdmime "mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type cachedStaticFile struct {
	content     []byte
	gzipContent []byte
	mimeType    string
	etag        string
	modTime     time.Time
}

type StaticServer struct {
	files map[string]*cachedStaticFile
}

func NewStaticServer(staticFS fs.FS) *StaticServer {
	srv := &StaticServer{
		files: make(map[string]*cachedStaticFile),
	}

	// 遍历 staticFS，预先将所有静态文件载入内存并预压缩
	_ = fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}

		cleanPath := strings.TrimPrefix(filepath.ToSlash(path), "static/")
		cleanPath = strings.TrimPrefix(cleanPath, "/")

		data, err := fs.ReadFile(staticFS, path)
		if err != nil {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		mimeType := stdmime.TypeByExtension(ext)
		if mimeType == "" {
			switch ext {
			case ".css":
				mimeType = "text/css; charset=utf-8"
			case ".js":
				mimeType = "application/javascript; charset=utf-8"
			case ".svg":
				mimeType = "image/svg+xml"
			case ".png":
				mimeType = "image/png"
			case ".jpg", ".jpeg":
				mimeType = "image/jpeg"
			case ".webp":
				mimeType = "image/webp"
			default:
				mimeType = "application/octet-stream"
			}
		}

		// 计算 sha256 作为强 ETag
		hash := sha256.Sum256(data)
		etag := `"` + hex.EncodeToString(hash[:16]) + `"`

		// 针对文本文件（CSS, JS, SVG, HTML）生成 gzip 预压缩内容
		var gzipBytes []byte
		if strings.HasPrefix(mimeType, "text/") ||
			strings.Contains(mimeType, "javascript") ||
			strings.Contains(mimeType, "json") ||
			strings.Contains(mimeType, "svg") {
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			_, _ = zw.Write(data)
			_ = zw.Close()
			if buf.Len() < len(data) {
				gzipBytes = buf.Bytes()
			}
		}

		cached := &cachedStaticFile{
			content:     data,
			gzipContent: gzipBytes,
			mimeType:    mimeType,
			etag:        etag,
			modTime:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		}

		srv.files[cleanPath] = cached
		return nil
	})

	return srv
}

func (s *StaticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	urlPath := strings.TrimPrefix(r.URL.Path, "/static/")
	urlPath = strings.TrimPrefix(urlPath, "/")

	file, ok := s.files[urlPath]
	if !ok {
		http.NotFound(w, r)
		return
	}

	// 1. 设置极速强缓存头（vendor 第三方固定库缓存 1 年，immutable 保证页面跳转绝不重新发起请求）
	if strings.HasPrefix(urlPath, "vendor/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	}

	w.Header().Set("ETag", file.etag)
	w.Header().Set("Vary", "Accept-Encoding")

	// 2. 协商缓存匹配：如果客户端有有效缓存，直接返回 304 Not Modified
	if match := r.Header.Get("If-None-Match"); match != "" {
		if strings.Contains(match, file.etag) || match == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	w.Header().Set("Content-Type", file.mimeType)

	// 3. Gzip 压缩流式传输
	acceptsGzip := strings.Contains(r.Header.Get("Accept-Encoding"), "gzip")
	if acceptsGzip && len(file.gzipContent) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = io.Copy(w, bytes.NewReader(file.gzipContent))
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	_, _ = io.Copy(w, bytes.NewReader(file.content))
}
