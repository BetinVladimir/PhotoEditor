// Package render compiles a timeline document (DS-09) into an ffmpeg command.
//
// DEMO MVP subset of docs/video/24-render-compiler.md: black canvas plus an
// overlay chain (D-37), fit/fill scaling, per-clip gain and mute, constant
// speed (video setpts, audio atempoChain), transitions fade_in / crossfade /
// dip_to_black (FN-E-02, D-40, D-41) with audio fades, mono->stereo via pan
// (D-45), amix with a silent base. Motion, transforms, effects, LUTs, ducking
// and two-pass loudness are rejected with an explicit code.
package render

import (
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/video/color"
)

const (
	TicksPerSecond = 705600000
	MaxInputs      = 128
	Version        = "render-compiler/demo-2"
)

// Source is what the worker learned from video_probe for a media/asset id.
type Source struct {
	DurationTicks int64
	HasVideo      bool
	HasAudio      bool
	Channels      int
	Matrix        string // bt709, bt601, smpte170m or bt470bg; "" means bt709
	HDR           string // pq or hlg; such video is tone-mapped to SDR (HDR_TONEMAPPED)
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
	Shortened     []Shortened       // VID-T-07 TRANSITION_SHORTENED
	Decisions     []string          // e.g. AUDIO_EFFECT_NOT_RENDERED:<clip>
	LUTs          map[string]string // file name -> .cube text, written next to the command (cwd of ffmpeg)
	AudioGraph    string            // audio part of FilterComplex, ends in [abus]; for the loudness pass
}

// Shortened records a transition the sources could not fully cover.
type Shortened struct {
	Clip      string
	Requested int64
	Effective int64
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

func fmtNum(x float64) string { return fmtNumD(x, 6) }

func fmtNumD(x float64, digits int) string {
	s := strconv.FormatFloat(x, 'f', digits, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// transformSteps is section 5.3: crop (returned to the canvas by pad), scale,
// rotate; the offset goes into overlay. Returns px, py and the chain suffix.
func transformSteps(tf map[string]any, W, H int64) (px, py, chain string) {
	pos, cr := arr(tf["position"]), arr(tf["crop"])
	px, py = fmtNum(flt(pos[0])), fmtNum(flt(pos[1]))
	l, t, r, b := flt(cr[0]), flt(cr[1]), flt(cr[2]), flt(cr[3])
	if l != 0 || t != 0 || r != 0 || b != 0 {
		chain += fmt.Sprintf(",crop=w=iw*(1-%s-%s):h=ih*(1-%s-%s):x=iw*%s:y=ih*%s,pad=%d:%d:%d*%s:%d*%s:color=black@0",
			fmtNum(l), fmtNum(r), fmtNum(t), fmtNum(b), fmtNum(l), fmtNum(t), W, H, W, fmtNum(l), H, fmtNum(t))
	}
	if sc := flt(tf["scale"]); tf["scale"] != nil && sc != 1 {
		chain += fmt.Sprintf(",scale=w='trunc(iw*%s/2)*2':h='trunc(ih*%s/2)*2'", fmtNum(sc), fmtNum(sc))
	}
	if a := flt(tf["rotate"]); a != 0 {
		e := fmtNum(a) + "*PI/180"
		chain += fmt.Sprintf(",rotate=a=%s:ow='rotw(%s)':oh='roth(%s)':c=none", e, e, e)
	}
	return
}

type duckCfg struct {
	sides  []string
	params string // threshold:ratio:attack:release options of sidechaincompress
}

type voice struct {
	label, track, chain string
	duck                *duckCfg
}

func duckNum(m map[string]any, k string, def float64) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return def
}

// duckParams is FN-R-13 duckParams.
func duckParams(thrDB, depth, attackMs, releaseMs float64) string {
	thr := math.Min(math.Max(math.Pow(10, thrDB/20), 0.000976563), 1)
	ratio := 1.0
	switch {
	case depth >= 11.4:
		ratio = 20
	case depth > 0:
		ratio = 1 / (1 - depth/12)
	}
	at := math.Min(math.Max(attackMs, 0.01), 2000)
	rel := math.Min(math.Max(releaseMs, 0.01), 9000)
	return fmt.Sprintf("threshold=%s:ratio=%s:attack=%s:release=%s", fmtNumD(thr, 9), fmtNumD(ratio, 6), fmtNumD(at, 3), fmtNumD(rel, 3))
}

func silence(D int64, n int) string {
	return fmt.Sprintf("anullsrc=r=48000:cl=stereo,atrim=duration=%s,asetpts=PTS-STARTPTS[sil%d]", FmtSeconds(D), n)
}

// audioGraph is section 6.2/6.3: the voices, the sidechain buses, the
// compressors and the final amix onto [abus].
func audioGraph(voices []voice, D int64) ([]string, string) {
	// Tracks used as a sidechain, with the number of ducking clips reading them.
	refs := map[string]int{}
	var order []string
	for _, v := range voices {
		if v.duck == nil {
			continue
		}
		for _, t := range v.duck.sides {
			if refs[t] == 0 {
				order = append(order, t)
			}
			refs[t]++
		}
	}
	var g []string
	for i := range voices {
		v := &voices[i]
		if v.duck != nil && len(v.duck.sides) > 0 && !hasVoiceOn(voices, v.duck.sides) {
			v.duck = nil
		}
	}
	for i, v := range voices {
		if refs[v.track] > 0 && !(v.duck != nil && contains(v.duck.sides, v.track)) {
			g = append(g, fmt.Sprintf("%s,asplit=2[%s][bs%s]", v.chain, v.label, strings.TrimPrefix(v.label, "av")))
		} else {
			g = append(g, fmt.Sprintf("%s[%s]", v.chain, v.label))
		}
		_ = i
	}
	sil := 0
	// One bus per sidechain track; asplit gives each referencing clip its own copy.
	buses := map[string][]string{}
	for _, t := range order {
		var in []string
		for _, v := range voices {
			if v.track == t {
				in = append(in, "[bs"+strings.TrimPrefix(v.label, "av")+"]")
			}
		}
		if len(in) == 0 {
			continue
		}
		sil++
		g = append(g, silence(D, sil))
		n := refs[t]
		outs := ""
		for j := 0; j < n; j++ {
			l := fmt.Sprintf("bus_%s_%d", t, j)
			buses[t] = append(buses[t], l)
			outs += "[" + l + "]"
		}
		g = append(g, fmt.Sprintf("[sil%d]%samix=inputs=%d:normalize=0:duration=first,asplit=%d%s", sil, strings.Join(in, ""), len(in)+1, n, outs))
	}
	final := make([]string, len(voices))
	used := map[string]int{}
	for i, v := range voices {
		final[i] = v.label
		if v.duck == nil {
			continue
		}
		var side []string
		for _, t := range v.duck.sides {
			if bl := buses[t]; len(bl) > 0 {
				side = append(side, bl[used[t]])
				used[t]++
			}
		}
		if len(side) == 0 {
			continue
		}
		sc := side[0]
		if len(side) > 1 {
			sil++
			g = append(g, silence(D, sil))
			sc = "scm" + strings.TrimPrefix(v.label, "av")
			g = append(g, fmt.Sprintf("[sil%d][%s]amix=inputs=%d:normalize=0:duration=first[%s]", sil, strings.Join(side, "]["), len(side)+1, sc))
		}
		du := "du" + strings.TrimPrefix(v.label, "av") + "_1"
		g = append(g, fmt.Sprintf("[%s][%s]sidechaincompress=%s:makeup=1:level_sc=1[%s]", v.label, sc, v.duck.params, du))
		final[i] = du
	}
	// Silent base keeps the audio bus (and the container) present for every doc.
	sil++
	g = append(g, silence(D, sil))
	mix := fmt.Sprintf("[sil%d]", sil)
	for _, l := range final {
		mix += "[" + l + "]"
	}
	g = append(g, fmt.Sprintf("%samix=inputs=%d:normalize=0:duration=first[abus]", mix, len(final)+1))
	return g, "abus"
}

func hasVoiceOn(voices []voice, tracks []string) bool {
	for _, v := range voices {
		if contains(tracks, v.track) {
			return true
		}
	}
	return false
}

func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}

const hdrTonemap = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"

var matrices = map[string]string{"": "bt709", "bt709": "bt709", "bt601": "bt601", "smpte170m": "bt601", "bt470bg": "bt601"}

func lutFilter(name string, alpha bool) string {
	f := fmt.Sprintf("scale=in_color_matrix=bt709:in_range=tv,format=rgba,lut3d=file='%s':interp=tetrahedral,scale=out_color_matrix=bt709:out_range=tv", name)
	if alpha {
		f += ",format=yuva420p"
	}
	return f
}

// LutName is FN-R-14: the file name of the folded LUT of the enabled colour effects, "" without any.
func LutName(effects []any) (string, error) {
	var list []any
	for _, e := range effects {
		if em := obj(e); em["enabled"] != false && strings.HasPrefix(str(em["type"]), "color.") {
			list = append(list, e)
		}
	}
	if len(list) == 0 {
		return "", nil
	}
	h, err := canonical.Hash(list)
	if err != nil {
		return "", err
	}
	return "lut_" + h[:16] + ".cube", nil
}

// addLUT folds the effects (FN-L-08), records the cube under its content-addressed name and returns the name.
func (p *Plan) addLUT(effects []any, luts map[string]*color.Lut3D, clip string) (string, error) {
	name, err := LutName(effects)
	if err != nil {
		return "", err
	}
	if _, done := p.LUTs[name]; done {
		return name, nil
	}
	var fx []color.Effect
	for _, e := range effects {
		em := obj(e)
		ce := color.Effect{Type: str(em["type"]), Params: obj(em["params"])}
		if ce.Type == "color.lut3d" {
			ce.LUT = luts[str(obj(ce.Params["lut"])["media_id"])]
			if ce.LUT == nil {
				return "", Error{Code: "COMPILE_SOURCE_MISSING", Clip: clip}
			}
		}
		fx = append(fx, ce)
	}
	l, err := color.Fold(fx, color.FoldSize)
	if err != nil {
		return "", Error{Code: "COMPILE_COLOR_UNSUPPORTED", Clip: clip}
	}
	if p.LUTs == nil {
		p.LUTs = map[string]string{}
	}
	p.LUTs[name] = color.WriteCube(l)
	return name, nil
}

// kbSupersample is FN-R-15: 4, reduced until max(W,H)*S fits in 8192.
func kbSupersample(w, h int64) int64 {
	s := int64(4)
	for s > 1 && max(w, h)*s > 8192 {
		s--
	}
	return s
}

// kbExpr is FN-R-11: zoompan z, x, y for a photo's pan_zoom motion. head is
// the number of frames the layer runs before the clip, n the clip's frames.
func kbExpr(clip map[string]any, head, n int64) (z, x, y string) {
	mo := obj(clip["motion"])
	u := fmt.Sprintf("min(max((on-%d)/%d,0),1)", head, n)
	e := u
	if str(mo["easing"]) == "ease_in_out" {
		e = fmt.Sprintf("(%s*%s*(3-2*%s))", u, u, u)
	}
	lerp := func(a, b float64) string {
		return fmt.Sprintf("(%s+(%s-(%s))*%s)", fmtNum(a), fmtNum(b), fmtNum(a), e)
	}
	f, t := obj(mo["from"]), obj(mo["to"])
	fc, tc := arr(f["center"]), arr(t["center"])
	z = lerp(flt(f["scale"]), flt(t["scale"]))
	x = fmt.Sprintf("max(0,min(iw-iw/zoom,%s*iw-iw/zoom/2))", lerp(flt(fc[0]), flt(tc[0])))
	y = fmt.Sprintf("max(0,min(ih-ih/zoom,%s*ih-ih/zoom/2))", lerp(flt(fc[1]), flt(tc[1])))
	return
}

// fadeBlack is D-41: fade the luma to broadcast black (Y=16), not Y=0.
func fadeBlack(dir string, st, d int64) string {
	return fmt.Sprintf("scale=in_range=tv:out_range=pc,fade=t=%s:st=%s:d=%s,scale=in_range=pc:out_range=tv", dir, FmtSeconds(st), FmtSeconds(d))
}

func fpsExpr(num, den int64) string {
	if den == 1 {
		return strconv.FormatInt(num, 10)
	}
	return fmt.Sprintf("%d/%d", num, den)
}

func speedOf(c map[string]any) (int64, int64) {
	if sp := obj(c["speed"]); sp != nil {
		return num(sp["num"]), num(sp["den"])
	}
	return 1, 1
}

// clipDuration is the timeline length (FN-T-04): source length divided by speed.
func clipDuration(c map[string]any) int64 {
	if str(obj(c["source"])["kind"]) == "photo" {
		return num(c["duration"])
	}
	n, d := speedOf(c)
	return mulDiv(num(c["src_out"])-num(c["src_in"]), d, n)
}

// mulDiv is a*b/c truncated, in arbitrary precision.
func mulDiv(a, b, c int64) int64 {
	r := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	return r.Quo(r, big.NewInt(c)).Int64()
}

// AtempoChain is FN-R-08 atempoChain: factors from [0.5; 2], the remainder
// at nine digits; nothing for 1/1.
func AtempoChain(num, den int64) []string {
	t := big.NewRat(num, den)
	two, half := big.NewRat(2, 1), big.NewRat(1, 2)
	var out []string
	for t.Cmp(two) > 0 {
		out = append(out, "2")
		t.Quo(t, two)
	}
	for t.Cmp(half) < 0 {
		out = append(out, "0.5")
		t.Mul(t, two)
	}
	if t.Cmp(big.NewRat(1, 1)) != 0 {
		s := new(big.Rat).Mul(t, big.NewRat(1000000000, 1))
		s.Add(s, half)
		n := new(big.Int).Quo(s.Num(), s.Denom())
		f := fmt.Sprintf("%010d", n)
		x := strings.TrimRight(f[len(f)-9:], "0")
		if x == "" {
			out = append(out, f[:len(f)-9])
		} else {
			out = append(out, f[:len(f)-9]+"."+x)
		}
	}
	return out
}

type trans struct {
	kind            string // cut, fade_in, crossfade, dip_to_black
	start, end, dur int64
	requested       int64
	shortened       bool
}

const unlimited = int64(1) << 62

// effectiveTransition is FN-E-02 for clips[i] of one video track.
func effectiveTransition(clips []any, i int, sources map[string]Source) trans {
	c := obj(clips[i])
	tr := obj(c["transition_in"])
	kind := str(tr["kind"])
	if tr == nil || kind == "" || kind == "cut" {
		return trans{kind: "cut"}
	}
	at, D := num(c["at"]), num(tr["duration"])
	prevOK := i > 0 && num(obj(clips[i-1])["at"])+clipDuration(obj(clips[i-1])) == at
	if (kind == "crossfade" || kind == "dip_to_black") && !prevOK {
		kind = "fade_in"
	}
	cut := trans{kind: "cut", start: at, end: at, requested: D, shortened: D > 0}
	switch kind {
	case "fade_in":
		d := D
		if dc := clipDuration(c); dc < d {
			d = dc
		}
		if d <= 0 {
			return trans{kind: "cut", start: at, end: at}
		}
		return trans{kind: kind, start: at, end: at + d, dur: d, requested: D, shortened: d < D}
	case "dip_to_black":
		half := D / 2
		if half <= 0 {
			return cut
		}
		return trans{kind: kind, start: at - half, end: at + half, dur: 2 * half, requested: D, shortened: 2*half < D}
	}
	head, tail := unlimited, unlimited
	if str(obj(c["source"])["kind"]) != "photo" {
		n, d := speedOf(c)
		head = mulDiv(num(c["src_in"]), d, n)
	}
	prev := obj(clips[i-1])
	if str(obj(prev["source"])["kind"]) != "photo" {
		n, d := speedOf(prev)
		tail = mulDiv(sources[sourceKey(prev)].DurationTicks-num(prev["src_out"]), d, n)
		if tail < 0 {
			tail = 0
		}
	}
	var t trans
	switch str(tr["align"]) {
	case "start":
		d := min(D, tail)
		t = trans{start: at, end: at + d, dur: d}
	case "end":
		d := min(D, head)
		t = trans{start: at - d, end: at, dur: d}
	default:
		half := min(D/2, head, tail)
		t = trans{start: at - half, end: at + half, dur: 2 * half}
	}
	if t.dur <= 0 {
		return cut
	}
	t.kind, t.requested, t.shortened = "crossfade", D, t.dur < D
	return t
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
func Compile(d Doc, sources map[string]Source) (Plan, error) { return CompileWith(d, sources, nil) }

// CompileWith is Compile with the parsed .cube files of the document's lut3d effects, keyed by media id.
func CompileWith(d Doc, sources map[string]Source, luts map[string]*color.Lut3D) (Plan, error) {
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
	var vchains, vlabels []string
	var voices []voice
	for _, t := range arr(d["tracks"]) {
		tm := obj(t)
		kind := str(tm["kind"])
		clips := arr(tm["clips"])
		for ci, c := range clips {
			cm := obj(c)
			id := str(cm["id"])
			if tr := obj(cm["transition_in"]); tr != nil {
				switch str(tr["kind"]) {
				case "", "cut", "fade_in", "crossfade", "dip_to_black":
				default:
					return Plan{}, Error{Code: "COMPILE_TRANSITION_UNSUPPORTED", Clip: id}
				}
			}
			mo := obj(cm["motion"])
			tf := obj(cm["transform"])
			if tf != nil && (len(arr(tf["position"])) != 2 || len(arr(tf["crop"])) != 4) {
				return Plan{}, Error{Code: "COMPILE_TRANSFORM_UNSUPPORTED", Clip: id}
			}
			if tf != nil && (tf["scale"] != nil && flt(tf["scale"]) <= 0 || mo != nil) || (mo != nil && (str(mo["kind"]) != "pan_zoom" || str(obj(cm["source"])["kind"]) != "photo")) {
				return Plan{}, Error{Code: "COMPILE_TRANSFORM_UNSUPPORTED", Clip: id}
			}
			sn, sd := speedOf(cm)
			if sn <= 0 || sd <= 0 || sn*1 > 4*sd || sd > 4*sn {
				return Plan{}, Error{Code: "COMPILE_SPEED_UNSUPPORTED", Clip: id}
			}
			var duck map[string]any
			var colorFx []any
			for _, e := range arr(cm["effects"]) {
				em := obj(e)
				if em["enabled"] == false {
					continue
				}
				switch ty := str(em["type"]); ty {
				case "audio.duck":
					duck = obj(em["params"])
				case "color.basic", "color.cdl", "color.curves", "color.lut3d":
					colorFx = append(colorFx, em)
				case "audio.gain", "audio.fade":
					p.Decisions = append(p.Decisions, "AUDIO_EFFECT_NOT_RENDERED:"+id)
				default:
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
			// Layer interval [l0, l1]: the clip plus the head/tail a crossfade
			// borrows from its neighbours (FN-E-02).
			var in, out trans
			if kind == "video" {
				in = effectiveTransition(clips, ci, sources)
				if ci+1 < len(clips) {
					out = effectiveTransition(clips, ci+1, sources)
				}
				if in.shortened {
					p.Shortened = append(p.Shortened, Shortened{Clip: id, Requested: in.requested, Effective: in.dur})
				}
			}
			l0, l1 := at, at+dur
			if in.kind == "crossfade" {
				l0 = in.start
			}
			if out.kind == "crossfade" && out.end > l1 {
				l1 = out.end
			}
			idx := len(p.Inputs)
			var opts []string
			if sk == "photo" {
				opts = []string{"-loop", "1", "-framerate", fps, "-t", FmtSeconds(l1 - l0)}
			} else {
				if !needV {
					opts = append(opts, "-vn")
				}
				if !needA {
					opts = append(opts, "-an")
				}
				opts = append(opts, "-ss", FmtSeconds(num(cm["src_in"])-mulDiv(at-l0, sn, sd)), "-t", FmtSeconds(mulDiv(l1-l0, sn, sd)))
			}
			p.Inputs = append(p.Inputs, Input{ClipID: id, Options: opts, Source: key})
			if needV {
				fit := str(cm["fit"])
				m, known := matrices[src.Matrix]
				hdr := src.HDR != "" && sk == "video"
				if (!known && !hdr) || (src.HDR != "" && src.HDR != "pq" && src.HDR != "hlg") {
					return Plan{}, Error{Code: "COMPILE_COLOR_UNSUPPORTED", Clip: id}
				}
				if hdr { // the tone-mapper outputs Rec.709, whatever matrix the source carried
					m = "bt709"
					p.Decisions = append(p.Decisions, "HDR_TONEMAPPED:"+id+":"+src.HDR)
				}
				cmx := "in_color_matrix=" + m + ":out_color_matrix=bt709:out_range=tv"
				var scale string
				switch fit {
				case "fill":
					scale = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase:force_divisible_by=2:%s,crop=%d:%d", W, H, cmx, W, H)
				case "none":
					if mo != nil {
						return Plan{}, Error{Code: "COMPILE_FIT_UNSUPPORTED", Clip: id}
					}
					scale = fmt.Sprintf("scale=%s,crop=w='min(iw,%d)':h='min(ih,%d)'", cmx, W, H)
				default:
					scale = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2:%s", W, H, cmx)
				}
				// D-39: layer starts on the frame grid, rounded up.
				start := (l0 + frame - 1) / frame * frame
				label := fmt.Sprintf("vc%d", idx)
				pts := "setpts=PTS-STARTPTS"
				if sk != "photo" && (sn != 1 || sd != 1) {
					pts = fmt.Sprintf("setpts=(PTS-STARTPTS)*%d/%d", sd, sn)
				}
				fpsF := "fps=" + fps
				cw, ch, pix := W, H, "yuva420p"
				if mo != nil {
					ss := kbSupersample(W, H)
					cw, ch, pix = W*ss, H*ss, "rgba"
					scale = strings.ReplaceAll(scale, fmt.Sprintf("%d:%d", W, H), fmt.Sprintf("%d:%d", cw, ch))
				}
				if hdr {
					fpsF += "," + hdrTonemap
				}
				chain := fmt.Sprintf("[%d:v]%s,%s,%s,format=%s,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black@0", idx, pts, fpsF, scale, pix, cw, ch)
				if mo != nil {
					z, x, y := kbExpr(cm, (at-l0)/frame, dur/frame)
					chain += fmt.Sprintf(",zoompan=z='%s':x='%s':y='%s':d=1:s=%dx%d:fps=%s,scale=out_color_matrix=bt709:out_range=tv,format=yuva420p", z, x, y, W, H, fps)
				}
				if len(colorFx) > 0 {
					name, err := p.addLUT(colorFx, luts, id)
					if err != nil {
						return Plan{}, err
					}
					chain += "," + lutFilter(name, true)
				}
				px, py := "0", "0"
				if tf != nil {
					var tch string
					px, py, tch = transformSteps(tf, W, H)
					chain += tch
				}
				switch in.kind {
				case "crossfade", "fade_in":
					chain += fmt.Sprintf(",fade=t=in:st=0:d=%s:alpha=1", FmtSeconds(in.dur))
				case "dip_to_black":
					chain += "," + fadeBlack("in", 0, in.dur/2)
				}
				if out.kind == "dip_to_black" {
					chain += "," + fadeBlack("out", out.start-l0, out.dur/2)
				}
				chain += fmt.Sprintf(",setpts=PTS-STARTPTS+%s/TB[%s]", FmtSeconds(start), label)
				vchains = append(vchains, chain)
				vlabels = append(vlabels, strings.Join([]string{label, FmtSeconds(start), FmtSeconds(l1), px, py}, "|"))
			}
			if needA {
				label := fmt.Sprintf("av%d", idx)
				chain := fmt.Sprintf("[%d:a]asetpts=PTS-STARTPTS,aresample=48000", idx)
				if src.Channels == 1 {
					chain += ",pan=stereo|c0=c0|c1=c0"
				}
				chain += ",aformat=sample_fmts=fltp:channel_layouts=stereo"
				for _, f := range AtempoChain(sn, sd) {
					chain += ",atempo=" + f
				}
				g := flt(tm["gain_db"])
				if ac != nil {
					g += flt(ac["gain_db"])
				}
				if g != 0 {
					chain += ",volume=" + fmtNumD(g, 4) + "dB:precision=float"
				}
				if ac != nil {
					if fi := num(ac["fade_in"]); fi > 0 {
						chain += fmt.Sprintf(",afade=t=in:st=%s:d=%s:curve=tri", FmtSeconds(at-l0), FmtSeconds(fi))
					}
					if fo := num(ac["fade_out"]); fo > 0 {
						chain += fmt.Sprintf(",afade=t=out:st=%s:d=%s:curve=tri", FmtSeconds(at+dur-fo-l0), FmtSeconds(fo))
					}
				}
				switch in.kind {
				case "crossfade":
					chain += fmt.Sprintf(",afade=t=in:st=0:d=%s:curve=tri", FmtSeconds(in.dur))
				case "dip_to_black":
					chain += fmt.Sprintf(",afade=t=in:st=0:d=%s:curve=tri", FmtSeconds(in.dur/2))
				}
				switch out.kind {
				case "crossfade":
					chain += fmt.Sprintf(",afade=t=out:st=%s:d=%s:curve=tri", FmtSeconds(out.start-l0), FmtSeconds(out.dur))
				case "dip_to_black":
					chain += fmt.Sprintf(",afade=t=out:st=%s:d=%s:curve=tri", FmtSeconds(out.start-l0), FmtSeconds(out.dur/2))
				}
				chain += fmt.Sprintf(",adelay=%dS:all=1", SamplesAt(l0)) // S = samples, not ms
				v := voice{label: label, track: str(tm["id"]), chain: chain}
				if duck != nil {
					v.duck = &duckCfg{params: duckParams(duckNum(duck, "threshold_db", -30), duckNum(duck, "depth_db", 12), duckNum(duck, "attack_ms", 20), duckNum(duck, "release_ms", 250))}
					for _, t := range arr(duck["sidechain_tracks"]) {
						v.duck.sides = append(v.duck.sides, str(t))
					}
				}
				voices = append(voices, v)
			}
		}
	}
	if len(p.Inputs) == 0 {
		return Plan{}, Error{Code: "COMPILE_EMPTY"}
	}
	canvas := fmt.Sprintf("color=c=black:s=%dx%d:r=%s:d=%s,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv[bg]", W, H, fps, FmtSeconds(D))
	g := append([]string{}, vchains...)
	g = append(g, canvas)
	prev := "bg"
	for i, vl := range vlabels {
		parts := strings.Split(vl, "|")
		next := fmt.Sprintf("ov%d", i)
		g = append(g, fmt.Sprintf("[%s][%s]overlay=x='(W-w)/2+(%s)*W':y='(H-h)/2+(%s)*H':eof_action=pass:enable='between(t,%s,%s)':format=auto[%s]", prev, parts[0], parts[3], parts[4], parts[1], parts[2], next))
		prev = next
	}
	var masterFx []any
	for _, e := range arr(obj(d["master"])["effects"]) {
		if em := obj(e); em["enabled"] != false && strings.HasPrefix(str(em["type"]), "color.") {
			masterFx = append(masterFx, em)
		}
	}
	if len(masterFx) > 0 {
		name, err := p.addLUT(masterFx, luts, "master")
		if err != nil {
			return Plan{}, err
		}
		g = append(g, fmt.Sprintf("[%s]%s,format=yuv420p[vout]", prev, lutFilter(name, false)))
	} else {
		g = append(g, fmt.Sprintf("[%s]format=yuv420p[vout]", prev))
	}
	ag, abus := audioGraph(voices, D)
	p.AudioGraph = strings.Join(ag, ";\n")
	g = append(g, ag...)
	_ = abus
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

// WriteLUTs writes the plan's .cube files into dir, the working directory of the ffmpeg command (FN-R-17).
func WriteLUTs(p Plan, dir string) error {
	for name, text := range p.LUTs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o640); err != nil {
			return err
		}
	}
	return nil
}
