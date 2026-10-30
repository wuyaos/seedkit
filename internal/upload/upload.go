// Package upload talks to image hosts. Each host returns the direct image URL.
package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Uploader interface {
	Name() string
	Upload(ctx context.Context, file string) (url string, err error)
}

type Result struct {
	File string `json:"file"`
	URL  string `json:"url"`
	Host string `json:"host"`
	Err  string `json:"error,omitempty"`
}

var client = &http.Client{Timeout: 120 * time.Second}

func multipartBody(file string, fields map[string]string, fileField string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	fw, err := w.CreateFormFile(fileField, filepath.Base(file))
	if err != nil {
		return nil, "", err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	if _, err := io.Copy(fw, f); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

func post(ctx context.Context, url string, body *bytes.Buffer, ctype string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "seedkit/1.0")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

// ---- picgo.net (Chevereto API v1) ----

type Picgo struct {
	Endpoint string
	APIKey   string
	Album    string
}

func (p *Picgo) Name() string { return "picgo" }

func (p *Picgo) Upload(ctx context.Context, file string) (string, error) {
	if p.APIKey == "" {
		return "", errors.New("picgo api key not configured")
	}
	fields := map[string]string{"format": "json"}
	if p.Album != "" {
		fields["album_id"] = p.Album
	}
	body, ctype, err := multipartBody(file, fields, "source")
	if err != nil {
		return "", err
	}
	ep := p.Endpoint
	if ep == "" {
		ep = "https://www.picgo.net/api/1/upload"
	}
	b, code, err := post(ctx, ep, body, ctype, map[string]string{"X-API-Key": p.APIKey})
	if err != nil {
		return "", err
	}
	var r struct {
		StatusCode int `json:"status_code"`
		Error      struct {
			Message string `json:"message"`
		} `json:"error"`
		Image struct {
			URL string `json:"url"`
		} `json:"image"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("picgo: http %d: %s", code, truncate(b))
	}
	if r.Image.URL == "" {
		if r.Error.Message != "" {
			return "", fmt.Errorf("picgo: %s", r.Error.Message)
		}
		return "", fmt.Errorf("picgo: http %d: %s", code, truncate(b))
	}
	return r.Image.URL, nil
}

// ---- pixhost.to ----

type Pixhost struct {
	ContentType int // 0 safe, 1 adult
	MaxTh       int
}

func (p *Pixhost) Name() string { return "pixhost" }

func (p *Pixhost) Upload(ctx context.Context, file string) (string, error) {
	maxTh := p.MaxTh
	if maxTh == 0 {
		maxTh = 350
	}
	fields := map[string]string{
		"content_type": fmt.Sprint(p.ContentType),
		"max_th_size":  fmt.Sprint(maxTh),
	}
	body, ctype, err := multipartBody(file, fields, "img")
	if err != nil {
		return "", err
	}
	b, code, err := post(ctx, "https://api.pixhost.to/images", body, ctype, map[string]string{"Accept": "application/json"})
	if err != nil {
		return "", err
	}
	var r struct {
		ShowURL string `json:"show_url"`
		ThURL   string `json:"th_url"`
		Error   struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &r); err != nil || r.ShowURL == "" {
		if r.Error.Description != "" {
			return "", fmt.Errorf("pixhost: %s", r.Error.Description)
		}
		return "", fmt.Errorf("pixhost: http %d: %s", code, truncate(b))
	}
	// show_url https://pixhost.to/show/123/abc.png → direct https://imgN.pixhost.to/images/123/abc.png
	direct := r.ShowURL
	if r.ThURL != "" {
		// th_url is https://tN.pixhost.to/thumbs/123/abc.png ; swap to img + images
		direct = strings.Replace(r.ThURL, "/thumbs/", "/images/", 1)
		direct = strings.Replace(direct, "://t", "://img", 1)
	} else {
		direct = strings.Replace(direct, "pixhost.to/show/", "img2.pixhost.to/images/", 1)
	}
	return direct, nil
}

func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// UploadAll uploads files sequentially with retries, reporting each result.
func UploadAll(ctx context.Context, up Uploader, files []string, onEach func(Result)) []Result {
	out := make([]Result, 0, len(files))
	for _, f := range files {
		var r Result
		r.File, r.Host = f, up.Name()
		var lastErr error
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil {
				lastErr = ctx.Err()
				break
			}
			u, err := up.Upload(ctx, f)
			if err == nil {
				r.URL = u
				lastErr = nil
				break
			}
			lastErr = err
			time.Sleep(time.Second * time.Duration(attempt+1))
		}
		if lastErr != nil {
			r.Err = lastErr.Error()
		}
		out = append(out, r)
		if onEach != nil {
			onEach(r)
		}
	}
	return out
}
