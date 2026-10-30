package screenshot

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
)

type Stream struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	PixFmt        string            `json:"pix_fmt"`
	ColorTransfer string            `json:"color_transfer"`
	ColorPrimaries string           `json:"color_primaries"`
	ColorSpace    string            `json:"color_space"`
	Tags          map[string]string `json:"tags"`
	Disposition   map[string]int    `json:"disposition"`
}

type Probe struct {
	Duration float64
	Video    *Stream
	Subs     []Stream // in subtitle-stream order (0:s:N)
	IsHDR    bool
	IsDoVi   bool
}

type probeOut struct {
	Streams []Stream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (s *Service) probe(ctx context.Context, file string, mpegPS bool) (*Probe, error) {
	args := []string{"-v", "error"}
	if mpegPS {
		args = append(args, "-analyzeduration", "100M", "-probesize", "100M")
	}
	args = append(args, "-show_entries",
		"format=duration:stream=index,codec_type,codec_name,width,height,pix_fmt,color_transfer,color_primaries,color_space:stream_tags=language,title:stream_disposition=default,forced",
		"-of", "json", file)
	out, err := exec.CommandContext(ctx, s.ffprobe, args...).Output()
	if err != nil {
		return nil, err
	}
	var po probeOut
	if err := json.Unmarshal(out, &po); err != nil {
		return nil, err
	}
	p := &Probe{}
	p.Duration, _ = strconv.ParseFloat(po.Format.Duration, 64)
	for i := range po.Streams {
		st := po.Streams[i]
		switch st.CodecType {
		case "video":
			if p.Video == nil && st.CodecName != "mjpeg" && st.CodecName != "png" {
				p.Video = &po.Streams[i]
			}
		case "subtitle":
			p.Subs = append(p.Subs, st)
		}
	}
	if p.Video != nil {
		switch p.Video.ColorTransfer {
		case "smpte2084", "arib-std-b67":
			p.IsHDR = true
		}
		if strings.Contains(p.Video.CodecName, "hevc") && strings.Contains(p.Video.PixFmt, "10") && p.Video.ColorPrimaries == "bt2020" {
			p.IsHDR = true
		}
	}
	return p, nil
}

func isTextSub(codec string) bool {
	switch strings.ToLower(codec) {
	case "srt", "subrip", "ass", "ssa", "webvtt", "mov_text", "text":
		return true
	}
	return false
}

// pickSubtitle mirrors bd.sh: "none" disables; "default"/"" picks default-or-first
// text track, else default-or-first graphic track; otherwise match language tag.
// Returns subtitle-relative index and kind ("text"|"graphic"), or -1.
func pickSubtitle(subs []Stream, lang string) (int, string) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "none" || lang == "off" {
		return -1, ""
	}
	kind := func(s Stream) string {
		if isTextSub(s.CodecName) {
			return "text"
		}
		return "graphic"
	}
	if lang == "" || lang == "default" || lang == "auto" {
		for _, want := range []string{"text", "graphic"} {
			for i, s := range subs {
				if kind(s) == want && s.Disposition["default"] == 1 {
					return i, want
				}
			}
			for i, s := range subs {
				if kind(s) == want {
					return i, want
				}
			}
		}
		return -1, ""
	}
	for i, s := range subs {
		if strings.EqualFold(s.Tags["language"], lang) {
			return i, kind(s)
		}
	}
	return -1, ""
}
