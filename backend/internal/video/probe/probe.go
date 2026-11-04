// Package probe turns ffprobe JSON into the facts the video subsystem stores
// (video_media.probe) and checks them against ingest limits.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"os/exec"
	"strconv"
	"strings"
)

const TicksPerSecond = 705600000

// Facts is the persisted probe summary.
type Facts struct {
	DurationTicks int64  `json:"duration_ticks"`
	Container     string `json:"container"`
	HasVideo      bool   `json:"has_video"`
	HasAudio      bool   `json:"has_audio"`
	Width         int    `json:"width,omitempty"`
	Height        int    `json:"height,omitempty"`
	FPSNum        int64  `json:"fps_num,omitempty"`
	FPSDen        int64  `json:"fps_den,omitempty"`
	VideoCodec    string `json:"video_codec,omitempty"`
	PixFmt        string `json:"pix_fmt,omitempty"`
	AudioCodec    string `json:"audio_codec,omitempty"`
	Channels      int    `json:"channels,omitempty"`
	SampleRate    int    `json:"sample_rate,omitempty"`
	ColorSpace    string `json:"color_space,omitempty"`    // ffprobe color_space (bt709, bt470bg, smpte170m, ...)
	ColorTransfer string `json:"color_transfer,omitempty"` // smpte2084 (PQ) or arib-std-b67 (HLG) mark HDR
}

// HDR returns "pq" or "hlg" for an HDR video stream, "" otherwise.
func (f Facts) HDR() string {
	switch f.ColorTransfer {
	case "smpte2084":
		return "pq"
	case "arib-std-b67":
		return "hlg"
	}
	return ""
}

// Reject codes (stable, stored in video_media.reject_code).
var (
	ErrUnreadable = errors.New("PROBE_UNREADABLE")
	ErrNoStreams  = errors.New("PROBE_NO_STREAMS")
	ErrTooLong    = errors.New("PROBE_TOO_LONG")
)

const maxDurationTicks = 7620480000000 // DS-01 MAX_DURATION_TICKS (3 h)

type raw struct {
	Format struct {
		Duration   string `json:"duration"`
		FormatName string `json:"format_name"`
	} `json:"format"`
	Streams []struct {
		CodecType     string `json:"codec_type"`
		CodecName     string `json:"codec_name"`
		Width         int    `json:"width"`
		Height        int    `json:"height"`
		RFrameRate    string `json:"r_frame_rate"`
		PixFmt        string `json:"pix_fmt"`
		ColorSpace    string `json:"color_space"`
		ColorTransfer string `json:"color_transfer"`
		Channels      int    `json:"channels"`
		SampleRate    string `json:"sample_rate"`
		Duration      string `json:"duration"`
		Disposition   struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

// ParseJSON converts `ffprobe -show_format -show_streams -of json` output.
func ParseJSON(b []byte) (Facts, error) {
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return Facts{}, ErrUnreadable
	}
	f := Facts{Container: r.Format.FormatName}
	dur := r.Format.Duration
	for _, s := range r.Streams {
		switch s.CodecType {
		case "video":
			if f.HasVideo || s.Disposition.AttachedPic == 1 {
				continue
			}
			f.HasVideo, f.Width, f.Height, f.VideoCodec, f.PixFmt = true, s.Width, s.Height, s.CodecName, s.PixFmt
			f.ColorSpace, f.ColorTransfer = s.ColorSpace, s.ColorTransfer
			f.FPSNum, f.FPSDen = parseRate(s.RFrameRate)
			if dur == "" {
				dur = s.Duration
			}
		case "audio":
			if f.HasAudio {
				continue
			}
			f.HasAudio, f.AudioCodec, f.Channels = true, s.CodecName, s.Channels
			f.SampleRate, _ = strconv.Atoi(s.SampleRate)
			if dur == "" {
				dur = s.Duration
			}
		}
	}
	if !f.HasVideo && !f.HasAudio {
		return f, ErrNoStreams
	}
	t, ok := secondsToTicks(dur)
	if !ok || t <= 0 {
		return f, ErrUnreadable
	}
	if t > maxDurationTicks {
		return f, ErrTooLong
	}
	f.DurationTicks = t
	return f, nil
}

func parseRate(s string) (int64, int64) {
	a, b, ok := strings.Cut(s, "/")
	if !ok {
		return 0, 0
	}
	n, e1 := strconv.ParseInt(a, 10, 64)
	d, e2 := strconv.ParseInt(b, 10, 64)
	if e1 != nil || e2 != nil || n <= 0 || d <= 0 {
		return 0, 0
	}
	return n, d
}

// secondsToTicks parses a decimal seconds string exactly (round half up).
func secondsToTicks(s string) (int64, bool) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok || r.Sign() < 0 {
		return 0, false
	}
	r.Mul(r, big.NewRat(TicksPerSecond, 1))
	r.Add(r, big.NewRat(1, 2))
	q := new(big.Int).Quo(r.Num(), r.Denom())
	if !q.IsInt64() {
		return 0, false
	}
	return q.Int64(), true
}

// Run executes ffprobe on path.
func Run(ctx context.Context, ffprobe, path string) (Facts, []byte, error) {
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_format", "-show_streams", "-of", "json", path).Output()
	if err != nil {
		return Facts{}, nil, ErrUnreadable
	}
	f, err := ParseJSON(out)
	return f, out, err
}
