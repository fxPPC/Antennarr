package media

import (
	"antennarr/internal/checkcpu"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	AudioCodec    string
	Codec         string
	Width, Height int
}

// Inspect and decode a small in-memory sample. This does not encode the output.
func Inspect(ctx context.Context, binary string, sample []byte) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	probe := filepath.Join(filepath.Dir(binary), "ffprobe")
	if filepath.Dir(binary) == "." {
		probe = "ffprobe"
	}
	cmd := checkcpu.Command(ctx, probe, "-v", "error", "-threads", "1", "-show_streams", "-show_entries", "stream=codec_type,codec_name,width,height", "-of", "json", "-i", "pipe:0")
	cmd.Stdin = bytes.NewReader(sample)
	raw, e := cmd.Output()
	if e != nil {
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}
		return Info{}, errors.New("media inspection failed")
	}
	var rows struct {
		Streams []struct {
			Type   string `json:"codec_type"`
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
	}
	if json.Unmarshal(raw, &rows) != nil {
		return Info{}, errors.New("invalid media inspection")
	}
	info := Info{}
	audio := false
	for _, s := range rows.Streams {
		if s.Type == "video" {
			info.Codec = s.Codec
			info.Width = s.Width
			info.Height = s.Height
		}
		if s.Type == "audio" {
			audio = true
			info.AudioCodec = s.Codec
		}
	}
	if info.Codec == "" || !audio {
		return info, errors.New("video or audio missing")
	}
	decode := checkcpu.Command(ctx, binary, "-nostdin", "-hide_banner", "-v", "error", "-filter_threads", "1", "-filter_complex_threads", "1", "-threads", "1", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-frames:v", "1", "-t", "2", "-progress", "pipe:1", "-f", "null", "-")
	decode.Stdin = bytes.NewReader(sample)
	progress, decodeErr := decode.Output()
	frames := 0
	for _, line := range strings.Split(string(progress), "\n") {
		if value, ok := strings.CutPrefix(line, "frame="); ok {
			n, _ := strconv.Atoi(strings.TrimSpace(value))
			if n > frames {
				frames = n
			}
		}
	}
	if decodeErr != nil || frames < 1 {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, errors.New("sample could not be decoded")
	}
	return info, nil
}
