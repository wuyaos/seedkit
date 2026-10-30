// Package bbcode renders user-editable text/template templates into forum posts.
package bbcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// Data is what templates see.
type Data struct {
	Title      string
	MediaInfo  string
	BDInfo     string
	BDInfoFull string
	Images     []string // direct URLs
	Thumbs     []string
	Torrent    string
	Source     string
	Extra      map[string]string
}

const DefaultName = "default"

const defaultTemplate = `[quote]{{if .BDInfo}}{{.BDInfo}}{{else}}{{.MediaInfo}}{{end}}[/quote]
{{range .Images}}[img]{{.}}[/img]
{{end}}`

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	_ = os.MkdirAll(dir, 0o755)
	return &Store{dir: dir}
}

type Template struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

func (s *Store) List() ([]Template, error) {
	ents, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := []Template{}
	seen := false
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		if name == DefaultName {
			seen = true
		}
		out = append(out, Template{Name: name, Body: string(b)})
	}
	if !seen {
		out = append(out, Template{Name: DefaultName, Body: defaultTemplate})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Store) Get(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(s.dir, safeName(name)+".tmpl"))
	if err != nil {
		if name == DefaultName {
			return defaultTemplate, nil
		}
		return "", err
	}
	return string(b), nil
}

func (s *Store) Save(name, body string) error {
	if _, err := template.New(name).Parse(body); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, safeName(name)+".tmpl"), []byte(body), 0o644)
}

func (s *Store) Delete(name string) error {
	if name == DefaultName {
		return errors.New("cannot delete default template")
	}
	return os.Remove(filepath.Join(s.dir, safeName(name)+".tmpl"))
}

func Render(body string, d Data) (string, error) {
	t, err := template.New("bbcode").Funcs(template.FuncMap{
		"json": func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"join": strings.Join,
	}).Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func safeName(n string) string {
	n = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '.' {
			return '_'
		}
		return r
	}, n)
	if n == "" {
		n = "_"
	}
	return n
}
