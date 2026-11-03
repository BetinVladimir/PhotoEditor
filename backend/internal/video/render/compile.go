// Package render compiles a timeline document (DS-09) into an ffmpeg command.
//
// DEMO MVP subset of docs/video/24-render-compiler.md: black canvas plus an
// overlay chain (D-37), fit/fill scaling, per-clip gain and mute, mono->stereo
// via pan (D-45), amix with a silent base. Transitions, motion, speed, LUTs,
// ducking and two-pass loudness are rejected or ignored with an explicit code.
package render

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	TicksPerSecond = 705600000
	MaxInputs      = 128
	Version        = "render-compiler/demo-1"
)

// Source is what the worker learned from video_probe for a media/asset id.
type Source struct {
	DurationTicks int64
	HasVideo      bool
	HasAudio      bool
	Channels      int
}

type Input struct {
	ClipID  string
	Options []string
	Source  string // key into the sources map; the worker substitutes the path
}

type Plan struct {
	Inputs        []Input
	FilterComplex string
	Maps          []string
	DurationTicks int64
	Version       string
}

// Error carries a stable COMPILE_* code.
type Error struct{ Code, Clip string }

func (e Error) Error() string {
	if e.Clip == "" {
		return e.Code
	}
	return e.Code + ":" + e.Clip
}

type Doc = map[string]any

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { a, _ := v.([]any); return a }
func str(v any) string         { s, _ := v.(string); return s }
func num(v any) int64          { f, _ := v.(float64); return int64(f) }
func flt(v any) float64        { f, _ := v.(float64); return f }

// FmtSeconds is FN-R-08 fmtSeconds: exact decimal, 9 digits, half up, trimmed.
func FmtSeconds(ticks int64) string {
	r := new(big.Rat).SetFrac(big.NewInt(ticks), big.NewInt(TicksPerSecond))
	r.Mul(r, big.NewRat(1000000000, 1))
	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom())
	s := n.String()
	for len(s) < 10 {
		s = "0" + s
	}
	i := s[:len(s)-9]
	f := strings.TrimRight(s[len(s)-9:], "0")
	if f == "" {
		return i
	}
	return i + "." + f
}

// SamplesAt is FN-R-08 samplesAt (48 kHz).
func SamplesAt(ticks int64) int64 {
	r := new(big.Int).Mul(big.NewInt(ticks), big.NewInt(48000))
	r.Mul(r, big.NewInt(2))
	r.Add(r, big.NewInt(TicksPerSecond))
	r.Quo(r, big.NewInt(2*TicksPerSecond))
	return r.Int64()
}

func fmtNum(x float64) string {
	s := strconv.FormatFloat(x, 'f', 6, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

func fpsExpr(num, den int64) string {
	if den == 1 {
		return strconv.FormatInt(num, 10)
	}
	return fmt.Sprintf("%d/%d", num, den)
}

func clipDuration(c map[string]any) int64 {
	if str(obj(c["source"])["kind"]) == "photo" {
		return num(c["duration"])
	}
	return num(c["src_out"]) - num(c["src_in"])
}

func docEnd(d Doc) int64 {
	var end int64
	for _, t := range arr(d["tracks"]) {
		for _, c := range arr(obj(t)["clips"]) {
			cm := obj(c)
			if e := num(cm["at"]) + clipDuration(cm); e > end {
				end = e
			}
		}
	}
	return end
}

func sourceKey(c map[string]any) string {
	s := obj(c["source"])
	if k := str(s["media_id"]); k != "" {
		return k
	}
	return str(s["asset_version_id"])
}

// Compile builds the plan. mode "audio" skips video chains (loudness pass).
func Compile(d Doc, sources map[string]Source) (Plan, error) {
	v := obj(d["video"])
	fr := obj(v["fps"])
	fn, fd := num(fr["num"]), num(fr["den"])
	W, H := num(v["width"]), num(v["height"])
	if fn <= 0 || fd <= 0 || W <= 0 || H <= 0 || W%2 != 0 || H%2 != 0 {
		return Plan{}, Error{Code: "COMPILE_FPS_UNSUPPORTED"}
	}
	D := docEnd(d)
	if D == 0 {
		return Plan{}, Error{Code: "COMPILE_EMPTY"}
	}
	fps := fpsExpr(fn, fd)
	frame := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(TicksPerSecond), big.NewInt(fd)), big.NewInt(fn)).Int64()

	var p Plan
	var chains, vlabels, alabels []string
	for _, t := range arr(d["tracks"]) {
		tm := obj(t)
		kind := str(tm["kind"])
		for _, c := range arr(tm["clips"]) {
			cm := obj(c)
			id := str(cm["id"])
			if tr := obj(cm["transition_in"]); tr != nil && str(tr["kind"]) != "" && str(tr["kind"]) != "cut" {
				return Plan{}, Error{Code: "COMPILE_TRANSITION_UNSUPPORTED", Clip: id}
			}
			if cm["motion"] != nil || cm["transform"] != nil {
				return Plan{}, Error{Code: "COMPILE_TRANSFORM_UNSUPPORTED", Clip: id}
			}
			if sp := obj(cm["speed"]); sp != nil && (num(sp["num"]) != 1 || num(sp["den"]) != 1) {
				return Plan{}, Error{Code: "COMPILE_SPEED_UNSUPPORTED", Clip: id}
			}
			for _, e := range arr(cm["effects"]) {
				if obj(e)["enabled"] != false {
					return Plan{}, Error{Code: "COMPILE_EFFECT_UNSUPPORTED", Clip: id}
				}
			}
			key := sourceKey(cm)
			src, ok := sources[key]
			if !ok {
				return Plan{}, Error{Code: "COMPILE_SOURCE_MISSING", Clip: id}
			}
			sk := str(obj(cm["source"])["kind"])
			at, dur := num(cm["at"]), clipDuration(cm)
			needV := sk != "audio" && kind == "video" && tm["visible"] != false && (sk == "photo" || src.HasVideo)
			ac := obj(cm["audio"])
			needA := sk != "photo" && src.HasAudio && tm["muted"] != true && !(ac != nil && ac["mute"] == true)
			if !needV && !needA {
				continue
			}
			if len(p.Inputs) >= MaxInputs {
				return Plan{}, Error{Code: "COMPILE_TOO_MANY_INPUTS"}
			}
			idx := len(p.Inputs)
			var opts []string
			if sk == "photo" {
				opts = []string{"-loop", "1", "-framerate", fps, "-t", FmtSeconds(dur)}
			} else {
				if !needV {
					opts = append(opts, "-vn")
				}
				if !needA {
					opts = append(opts, "-an")
				}
				opts = append(opts, "-ss", FmtSeconds(num(cm["src_in"])), "-t", FmtSeconds(dur))
			}
			p.Inputs = append(p.Inputs, Input{ClipID: id, Options: opts, Source: key})
			if needV {
				fit := str(cm["fit"])
				var scale string
				switch fit {
				case "fill":
					scale = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:force_divisible_by=2:out_color_matrix=bt709:out_range=tv,crop=%d:%d", W, H, W, H)
				case "none":
					return Plan{}, Error{Code: "COMPILE_FIT_UNSUPPORTED", Clip: id}
				default:
					scale = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2:out_color_matrix=bt709:out_range=tv", W, H)
				}
				// D-39: layer starts on the frame grid, rounded up.
				start := (at + frame - 1) / frame * frame
				label := fmt.Sprintf("vc%d", idx)
				chains = append(chains, fmt.Sprintf("[%d:v]setpts=PTS-STARTPTS,fps=%s,%s,format=yuva420p,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black@0,setpts=PTS-STARTPTS+%s/TB[%s]",
					idx, fps, scale, W, H, FmtSeconds(start), label))
				vlabels = append(vlabels, label+"|"+FmtSeconds(start)+"|"+FmtSeconds(at+dur))
			}
			if needA {
				label := fmt.Sprintf("ac%d", idx)
				ch := "aformat=sample_fmts=fltp:channel_layouts=stereo"
				if src.Channels == 1 {
					ch = "pan=stereo|c0=c0|c1=c0,aformat=sample_fmts=fltp"
				}
				chain := fmt.Sprintf("[%d:a]asetpts=PTS-STARTPTS,aresample=48000,%s", idx, ch)
				if ac != nil && flt(ac["gain_db"]) != 0 {
					chain += ",volume=" + fmtNum(flt(ac["gain_db"])) + "dB"
				}
				if g := flt(tm["gain_db"]); g != 0 {
					chain += ",volume=" + fmtNum(g) + "dB"
				}
				chain += fmt.Sprintf(",adelay=%dS:all=1[%s]", SamplesAt(at), label) // S = samples, not ms
				chains = append(chains, chain)
				alabels = append(alabels, label)
			}
		}
	}
	if len(p.Inputs) == 0 {
		return Plan{}, Error{Code: "COMPILE_EMPTY"}
	}
	canvas := fmt.Sprintf("color=c=black:s=%dx%d:r=%s:d=%s,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[bg]", W, H, fps, FmtSeconds(D))
	g := []string{canvas}
	g = append(g, chains...)
	prev := "bg"
	for i, vl := range vlabels {
		parts := strings.Split(vl, "|")
		next := fmt.Sprintf("ov%d", i)
		g = append(g, fmt.Sprintf("[%s][%s]overlay=x='(W-w)/2':y='(H-h)/2':eof_action=pass:enable='between(t,%s,%s)':format=auto[%s]", prev, parts[0], parts[1], parts[2], next))
		prev = next
	}
	g = append(g, fmt.Sprintf("[%s]format=yuv420p[vout]", prev))
	// Silent base keeps the audio bus (and the container) present for every doc.
	g = append(g, fmt.Sprintf("anullsrc=r=48000:cl=stereo:d=%s[abase]", FmtSeconds(D)))
	mix := "[abase]"
	for _, l := range alabels {
		mix += "[" + l + "]"
	}
	g = append(g, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=first[abus]", mix, len(alabels)+1))
	p.FilterComplex = strings.Join(g, ";\n")
	p.Maps = []string{"[vout]", "[abus]"}
	p.DurationTicks = D
	p.Version = Version
	return p, nil
}

// Args builds the ffmpeg argv. paths maps Input.Source to a local file.
// encoder is "libx264" or "h264_nvenc".
func Args(p Plan, paths map[string]string, out, encoder string, fn, fd int64) ([]string, error) {
	a := []string{"-hide_banner", "-nostdin", "-y", "-progress", "pipe:1"}
	for _, in := range p.Inputs {
		path, ok := paths[in.Source]
		if !ok {
			return nil, Error{Code: "COMPILE_SOURCE_MISSING", Clip: in.ClipID}
		}
		a = append(a, in.Options...)
		a = append(a, "-i", path)
	}
	a = append(a, "-filter_complex", p.FilterComplex, "-map", p.Maps[0], "-map", p.Maps[1], "-t", FmtSeconds(p.DurationTicks))
	switch encoder {
	case "h264_nvenc":
		a = append(a, "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-cq", "23", "-b:v", "0")
	case "libx264":
		a = append(a, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20")
	default:
		return nil, Error{Code: "COMPILE_ENCODER_UNSUPPORTED"}
	}
	a = append(a, "-pix_fmt", "yuv420p", "-r", fpsExpr(fn, fd), "-c:a", "aac", "-b:a", "192k", "-ar", "48000", "-movflags", "+faststart", out)
	return a, nil
}
