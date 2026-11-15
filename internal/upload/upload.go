// Package upload coordinates image-host uploads.
package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Uploader interface {
	Name() string
	Upload(context.Context, string) (string, error)
}
type Image struct {
	URL      string `json:"url"`
	ThumbURL string `json:"thumb_url,omitempty"`
	PageURL  string `json:"page_url,omitempty"`
}

// imageLink accepts the two response shapes used by Chevereto hosts.
type imageLink string

func (l *imageLink) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = imageLink(s)
		return nil
	}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	*l = imageLink(obj.URL)
	return nil
}

type details interface {
	UploadDetails(context.Context, string) (Image, error)
}
type Result struct {
	File      string `json:"file"`
	URL       string `json:"url"`
	Host      string `json:"host"`
	Err       string `json:"error,omitempty"`
	Index     int    `json:"index"`
	ThumbURL  string `json:"thumb_url,omitempty"`
	PageURL   string `json:"page_url,omitempty"`
	Status    string `json:"status"`
	Attempts  int    `json:"attempts"`
	Cached    bool   `json:"cached"`
	Uncertain bool   `json:"uncertain,omitempty"`
}
type Options struct {
	Concurrency int
	Force       bool
}
type Engine struct {
	path  string
	mu    sync.Mutex
	cache map[string]Image
}
type UploadError struct {
	Host       string
	StatusCode int
	RetryAfter time.Duration
	Retryable  bool
	Uncertain  bool
	Cause      error
}

func (e *UploadError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: upload failed (http %d)", e.Host, e.StatusCode)
	}
	if e.Uncertain {
		return e.Host + ": upload outcome uncertain"
	}
	return e.Host + ": upload failed"
}
func (e *UploadError) Unwrap() error { return e.Cause }

var client = &http.Client{Timeout: 120 * time.Second}

func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
func multipartBody(file string, fields map[string]string, field string) (*bytes.Buffer, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for k, v := range fields {
		if e := w.WriteField(k, v); e != nil {
			return nil, "", e
		}
	}
	f, e := os.Open(file)
	if e != nil {
		return nil, "", e
	}
	defer f.Close()
	fw, e := w.CreateFormFile(field, filepath.Base(file))
	if e != nil {
		return nil, "", e
	}
	if _, e = io.Copy(fw, f); e != nil {
		return nil, "", e
	}
	if e = w.Close(); e != nil {
		return nil, "", e
	}
	return &b, w.FormDataContentType(), nil
}
func after(s string) time.Duration {
	if n, e := strconv.Atoi(strings.TrimSpace(s)); e == nil && n >= 0 {
		if n > 30 {
			n = 30
		}
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(s); e == nil {
		return max(0, min(time.Until(t), 30*time.Second))
	}
	return 0
}
func req(ctx context.Context, host, method, ep string, b *bytes.Buffer, ct string, h map[string]string) ([]byte, int, http.Header, error) {
	r, e := http.NewRequestWithContext(ctx, method, ep, b)
	if e != nil {
		return nil, 0, nil, &UploadError{Host: host, Cause: e}
	}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	r.Header.Set("User-Agent", "seedkit/1.0")
	for k, v := range h {
		r.Header.Set(k, v)
	}
	// Credentials must never be forwarded to a redirect destination.
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := c.Do(r)
	if e != nil {
		uncertain := !errors.Is(e, context.Canceled)
		if errors.Is(e, context.DeadlineExceeded) {
			uncertain = true
		}
		var ne net.Error
		if errors.As(e, &ne) {
			uncertain = true
		}
		return nil, 0, nil, &UploadError{Host: host, Uncertain: uncertain, Cause: e}
	}
	defer resp.Body.Close()
	d, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if e != nil {
		return nil, resp.StatusCode, resp.Header, &UploadError{Host: host, Uncertain: true, Cause: e}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return d, resp.StatusCode, resp.Header, &UploadError{Host: host, StatusCode: resp.StatusCode, Retryable: resp.StatusCode == 429 || resp.StatusCode >= 500 && resp.StatusCode < 600, RetryAfter: after(resp.Header.Get("Retry-After"))}
	}
	return d, resp.StatusCode, resp.Header, nil
}
func malformed(host string) error {
	return &UploadError{Host: host, Cause: errors.New("invalid response")}
}

type Picgo struct{ Endpoint, APIKey, Album string }

func (*Picgo) Name() string { return "picgo" }
func (p *Picgo) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}
func (p *Picgo) UploadDetails(c context.Context, f string) (Image, error) {
	if p.APIKey == "" {
		return Image{}, errors.New("picgo api key not configured")
	}
	fields := map[string]string{"format": "json"}
	if p.Album != "" {
		fields["album_id"] = p.Album
	}
	b, t, e := multipartBody(f, fields, "source")
	if e != nil {
		return Image{}, e
	}
	ep := p.Endpoint
	if ep == "" {
		ep = "https://www.picgo.net/api/1/upload"
	}
	d, n, _, e := req(c, "picgo", "POST", ep, b, t, map[string]string{"X-API-Key": p.APIKey})
	if e != nil {
		return Image{}, e
	}
	var r struct {
		Status int `json:"status_code"`
		Image  struct {
			URL   string `json:"url"`
			Thumb struct {
				URL string `json:"url"`
			} `json:"thumb"`
		} `json:"image"`
		Page string `json:"url_viewer"`
	}
	if json.Unmarshal(d, &r) != nil || n < 200 || n >= 300 || r.Status != 200 || !validURL(r.Image.URL) {
		return Image{}, malformed("picgo")
	}
	return Image{r.Image.URL, r.Image.Thumb.URL, r.Page}, nil
}

type Pixhost struct {
	ContentType, MaxTh int
	Domain             string
}

func (*Pixhost) Name() string { return "pixhost" }
func (p *Pixhost) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}
func (p *Pixhost) UploadDetails(c context.Context, f string) (Image, error) {
	m := p.MaxTh
	if m == 0 {
		m = 350
	}
	b, t, e := multipartBody(f, map[string]string{"content_type": strconv.Itoa(p.ContentType), "max_th_size": strconv.Itoa(m)}, "img")
	if e != nil {
		return Image{}, e
	}
	domain := p.Domain
	if domain == "" {
		domain = "to"
	}
	if domain != "to" && domain != "cc" {
		return Image{}, errors.New("unsupported Pixhost domain")
	}
	d, n, _, e := req(c, "pixhost", "POST", "https://api.pixhost."+domain+"/images", b, t, map[string]string{"Accept": "application/json"})
	if e != nil {
		return Image{}, e
	}
	var r struct {
		Show  string `json:"show_url"`
		Thumb string `json:"th_url"`
	}
	if json.Unmarshal(d, &r) != nil || n < 200 || n >= 300 || !validURL(r.Show) || !validURL(r.Thumb) {
		return Image{}, malformed("pixhost")
	}
	u, _ := url.Parse(r.Thumb)
	host := u.Host
	if strings.HasPrefix(host, "t") {
		host = "img" + strings.TrimPrefix(host, "t")
	}
	u.Host = host
	u.Path = strings.Replace(u.Path, "/thumbs/", "/images/", 1)
	direct := u.String()
	if !validURL(direct) {
		return Image{}, malformed("pixhost")
	}
	return Image{direct, r.Thumb, r.Show}, nil
}

type Imgbb struct{ APIKey string }

func (*Imgbb) Name() string { return "imgbb" }
func (p *Imgbb) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}
func (p *Imgbb) UploadDetails(c context.Context, f string) (Image, error) {
	if p.APIKey == "" {
		return Image{}, errors.New("imgbb api key not configured")
	}
	b, t, e := multipartBody(f, map[string]string{"key": p.APIKey}, "image")
	if e != nil {
		return Image{}, e
	}
	d, n, _, e := req(c, "imgbb", "POST", "https://api.imgbb.com/1/upload", b, t, nil)
	if e != nil {
		return Image{}, e
	}
	var r struct {
		Success bool `json:"success"`
		Data    struct {
			URL     string `json:"url"`
			Display string `json:"display_url"`
			Viewer  string `json:"url_viewer"`
			Thumb   struct {
				URL string `json:"url"`
			} `json:"thumb"`
		} `json:"data"`
	}
	if json.Unmarshal(d, &r) != nil || n < 200 || n >= 300 || !r.Success || !validURL(r.Data.URL) {
		return Image{}, malformed("imgbb")
	}
	return Image{r.Data.URL, r.Data.Thumb.URL, r.Data.Viewer}, nil
}

type Freeimage struct{ APIKey string }

func (*Freeimage) Name() string { return "freeimage" }
func (p *Freeimage) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}
func (p *Freeimage) UploadDetails(c context.Context, f string) (Image, error) {
	if p.APIKey == "" {
		return Image{}, errors.New("freeimage api key not configured")
	}
	b, t, e := multipartBody(f, map[string]string{"key": p.APIKey, "action": "upload", "format": "json"}, "source")
	if e != nil {
		return Image{}, e
	}
	d, n, _, e := req(c, "freeimage", "POST", "https://freeimage.host/api/1/upload", b, t, nil)
	if e != nil {
		return Image{}, e
	}
	var r struct {
		Status int `json:"status_code"`
		Image  struct {
			URL   string    `json:"url"`
			Thumb imageLink `json:"thumb"`
			Page  string    `json:"url_viewer"`
		} `json:"image"`
	}
	if json.Unmarshal(d, &r) != nil || n < 200 || n >= 300 || r.Status != 200 || !validURL(r.Image.URL) {
		return Image{}, malformed("freeimage")
	}
	return Image{r.Image.URL, string(r.Image.Thumb), r.Image.Page}, nil
}

type AGSV struct{ Token string }

func (*AGSV) Name() string { return "agsv" }
func (p *AGSV) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}
func (p *AGSV) UploadDetails(c context.Context, f string) (Image, error) {
	if p.Token == "" {
		return Image{}, errors.New("agsv token not configured")
	}
	b, t, e := multipartBody(f, nil, "file")
	if e != nil {
		return Image{}, e
	}
	d, n, _, e := req(c, "agsv", "POST", "https://img.seedvault.cn/api/v1/upload", b, t, map[string]string{"Authorization": "Bearer " + p.Token, "Accept": "application/json"})
	if e != nil {
		return Image{}, e
	}
	var r struct {
		Data struct {
			Links struct {
				URL string `json:"url"`
			} `json:"links"`
		} `json:"data"`
	}
	if json.Unmarshal(d, &r) != nil || n < 200 || n >= 300 || !validURL(r.Data.Links.URL) {
		return Image{}, malformed("agsv")
	}
	return Image{URL: r.Data.Links.URL}, nil
}

type Pterclub struct{ Cookie string }

func (*Pterclub) Name() string { return "pterclub" }
func (p *Pterclub) Upload(c context.Context, f string) (string, error) {
	i, e := p.UploadDetails(c, f)
	return i.URL, e
}

var tokenRE = regexp.MustCompile(`PF\.obj\.config\.auth_token\s*=\s*["']([^"']+)["']`)
var tokenAlt = regexp.MustCompile(`auth_token["']?\s*[:=]\s*["']([a-zA-Z0-9]+)["']`)

func (p *Pterclub) UploadDetails(ctx context.Context, f string) (Image, error) {
	if p.Cookie == "" {
		return Image{}, errors.New("pterclub cookie not configured")
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for refresh := 0; refresh < 2; refresh++ {
		token, e := pterToken(ctx, p.Cookie, &c)
		if e != nil {
			return Image{}, e
		}
		im, e := pterPost(ctx, f, p.Cookie, token, &c)
		if e == nil {
			return im, nil
		}
		var ue *UploadError
		if refresh == 0 && errors.As(e, &ue) && (ue.StatusCode == 401 || ue.StatusCode == 403 || ue.StatusCode == 200 && ue.Cause != nil && ue.Cause.Error() == "token expired") {
			continue
		}
		return Image{}, e
	}
	return Image{}, malformed("pterclub")
}
func pterToken(ctx context.Context, cookie string, c *http.Client) (string, error) {
	r, e := http.NewRequestWithContext(ctx, "GET", "https://s3.pterclub.net/", nil)
	if e != nil {
		return "", malformed("pterclub")
	}
	r.Header.Set("Cookie", cookie)
	x, e := c.Do(r)
	if e != nil {
		return "", &UploadError{Host: "pterclub", Cause: e}
	}
	defer x.Body.Close()
	if x.StatusCode != 200 {
		return "", &UploadError{Host: "pterclub", StatusCode: x.StatusCode, Retryable: x.StatusCode == 429 || x.StatusCode >= 500 && x.StatusCode < 600, RetryAfter: after(x.Header.Get("Retry-After"))}
	}
	b, e := io.ReadAll(io.LimitReader(x.Body, 2<<20))
	if e != nil {
		return "", malformed("pterclub")
	}
	m := tokenRE.FindStringSubmatch(string(b))
	if len(m) == 0 {
		m = tokenAlt.FindStringSubmatch(string(b))
	}
	if len(m) == 0 {
		return "", malformed("pterclub")
	}
	return m[1], nil
}
func pterPost(ctx context.Context, f, cookie, token string, c *http.Client) (Image, error) {
	b, t, e := multipartBody(f, map[string]string{"type": "file", "action": "upload", "timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10), "auth_token": token, "nsfw": "0"}, "source")
	if e != nil {
		return Image{}, e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", "https://s3.pterclub.net/json", b)
	if e != nil {
		return Image{}, malformed("pterclub")
	}
	r.Header.Set("Content-Type", t)
	r.Header.Set("Cookie", cookie)
	x, e := c.Do(r)
	if e != nil {
		return Image{}, &UploadError{Host: "pterclub", Uncertain: true, Cause: e}
	}
	defer x.Body.Close()
	d, e := io.ReadAll(io.LimitReader(x.Body, 1<<20))
	if e != nil {
		return Image{}, &UploadError{Host: "pterclub", Uncertain: true, Cause: e}
	}
	var z struct {
		Status int `json:"status_code"`
		Image  struct {
			URL   string    `json:"url"`
			Thumb imageLink `json:"thumb"`
			Page  string    `json:"url_viewer"`
		} `json:"image"`
	}
	if x.StatusCode < 200 || x.StatusCode >= 300 {
		return Image{}, &UploadError{Host: "pterclub", StatusCode: x.StatusCode, Retryable: x.StatusCode == 429 || x.StatusCode >= 500 && x.StatusCode < 600, RetryAfter: after(x.Header.Get("Retry-After"))}
	}
	if json.Unmarshal(d, &z) != nil {
		return Image{}, malformed("pterclub")
	}
	if z.Status != 200 {
		cause := errors.New("invalid response")
		if z.Status == 403 {
			cause = errors.New("token expired")
		}
		return Image{}, &UploadError{Host: "pterclub", StatusCode: 200, Cause: cause}
	}
	if !validURL(z.Image.URL) {
		return Image{}, malformed("pterclub")
	}
	return Image{z.Image.URL, string(z.Image.Thumb), z.Image.Page}, nil
}
func NewEngine(path string) *Engine {
	e := &Engine{path: path, cache: map[string]Image{}}
	if path != "" {
		if b, x := os.ReadFile(path); x == nil {
			_ = json.Unmarshal(b, &e.cache)
		}
	}
	if e.cache == nil {
		e.cache = map[string]Image{}
	}
	return e
}
func identity(u Uploader) string {
	b, _ := json.Marshal(u)
	h := sha256.Sum256(append([]byte(u.Name()+"\x00"), b...))
	return hex.EncodeToString(h[:])
}
func (e *Engine) save() error {
	if e.path == "" {
		return nil
	}
	b, x := json.Marshal(e.cache)
	if x != nil {
		return x
	}
	if x = os.MkdirAll(filepath.Dir(e.path), 0700); x != nil {
		return x
	}
	tmp := e.path + ".tmp"
	if x = os.WriteFile(tmp, b, 0600); x != nil {
		return x
	}
	return os.Rename(tmp, e.path)
}

type task struct {
	key     string
	indices []int
}
type completed struct {
	task      task
	image     Image
	err       error
	attempts  int
	uncertain bool
}

func (e *Engine) UploadAll(ctx context.Context, u Uploader, files []string, opt Options, cb func(Result)) []Result {
	out := make([]Result, len(files))
	if len(files) == 0 {
		return out
	}
	id := identity(u)
	keys := make([]string, len(files))
	for i, f := range files {
		if ctx.Err() != nil {
			out[i] = Result{File: f, Host: u.Name(), Index: i}
			continue
		}
		out[i] = Result{File: f, Host: u.Name(), Index: i}
		if b, x := os.ReadFile(f); x == nil {
			h := sha256.Sum256(b)
			keys[i] = id + ":" + hex.EncodeToString(h[:])
		}
	}
	e.mu.Lock()
	cached := make(map[string]Image)
	for k, v := range e.cache {
		cached[k] = v
	}
	e.mu.Unlock()
	groups := map[string]*task{}
	order := []string{}
	for i, k := range keys {
		if ctx.Err() == nil && !opt.Force && k != "" {
			if im, ok := cached[k]; ok && validURL(im.URL) {
				out[i].URL = im.URL
				out[i].ThumbURL = im.ThumbURL
				out[i].PageURL = im.PageURL
				out[i].Status = "cached"
				out[i].Cached = true
				continue
			}
		}
		gk := k
		if gk == "" {
			gk = fmt.Sprintf("index:%d", i)
		}
		if groups[gk] == nil {
			groups[gk] = &task{key: k}
			order = append(order, gk)
		}
		groups[gk].indices = append(groups[gk].indices, i)
	}
	limit := opt.Concurrency
	if limit > 8 {
		limit = 8
	}
	if limit < 1 {
		limit = 3
	}
	jobs := make(chan task)
	results := make(chan completed, len(order))
	var workers sync.WaitGroup
	worker := func() {
		defer workers.Done()
		for t := range jobs {
			c := completed{task: t}
			if ctx.Err() != nil {
				c.err = ctx.Err()
				results <- c
				continue
			}
			for a := 1; a <= 3; a++ {
				c.attempts = a
				var im Image
				var er error
				if d, ok := u.(details); ok {
					im, er = d.UploadDetails(ctx, out[t.indices[0]].File)
				} else {
					im.URL, er = u.Upload(ctx, out[t.indices[0]].File)
				}
				if er == nil && validURL(im.URL) {
					c.image = im
					break
				}
				c.err = er
				var ue *UploadError
				if !errors.As(er, &ue) || !ue.Retryable || ue.Uncertain {
					if ue != nil {
						c.uncertain = ue.Uncertain
					}
					break
				}
				delay := ue.RetryAfter
				if delay == 0 {
					delay = time.Duration(a) * 100 * time.Millisecond
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					c.err = ctx.Err()
					break
				case <-timer.C:
				}
				if ctx.Err() != nil {
					break
				}
			}
			results <- c
		}
	}
	if limit > len(order) {
		limit = len(order)
	}
	workers.Add(limit)
	for i := 0; i < limit; i++ {
		go worker()
	}
	go func() {
		for _, k := range order {
			jobs <- *groups[k]
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()
	for c := range results {
		for _, i := range c.task.indices {
			r := &out[i]
			r.Attempts = c.attempts
			r.Uncertain = c.uncertain
			if ctx.Err() != nil && c.image.URL == "" {
				r.Status = "cancelled"
				r.Err = "context canceled"
			} else if c.image.URL != "" {
				r.URL = c.image.URL
				r.ThumbURL = c.image.ThumbURL
				r.PageURL = c.image.PageURL
				r.Status = "success"
			} else {
				r.Status = "failed"
				if c.err != nil {
					r.Err = c.err.Error()
				} else {
					r.Err = "upload failed"
				}
			}
		}
		if c.image.URL != "" && c.task.key != "" {
			e.mu.Lock()
			e.cache[c.task.key] = c.image
			saveErr := e.save()
			e.mu.Unlock()
			if saveErr != nil {
				for _, i := range c.task.indices {
					out[i].Err = "upload succeeded but cache persistence failed"
				}
			}
		}
		for j, i := range c.task.indices {
			if j > 0 {
				if c.image.URL != "" {
					out[i].Status = "cached"
					out[i].Cached = true
				} else {
					out[i].Status = out[c.task.indices[0]].Status
					out[i].Err = out[c.task.indices[0]].Err
				}
			}
			if cb != nil {
				cb(out[i])
			}
		}
	}
	for i := range out {
		if out[i].Status == "cached" && out[i].Attempts == 0 && cb != nil {
			cb(out[i])
		}
	}
	return out
}
func UploadAll(c context.Context, u Uploader, files []string, cb func(Result)) []Result {
	return NewEngine("").UploadAll(c, u, files, Options{}, cb)
}
