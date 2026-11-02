package timeline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Issue is one validator finding (DS-10).
type Issue struct {
	Rule    string `json:"rule"`
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
}

// Context is ValidateContext (DS-10).
type Context struct {
	SourceDurations map[string]int64
	SourceStates    map[string]string
	SourceHashes    map[string]string
	MusicClipIDs    []string
	ForRender       bool
}

const (
	maxIssues       = 100
	MaxDocBytes     = 4 * 1024 * 1024
	MaxDurationTick = 3 * 3600 * TicksPerSecond
)

var (
	idRE         = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	supportedFPS = map[string]bool{"24/1": true, "25/1": true, "30/1": true, "50/1": true, "60/1": true, "24000/1001": true, "30000/1001": true, "60000/1001": true}
	closedKeys   = map[string][]string{
		"root":        {"schema_version", "sequence_id", "base_revision", "timebase", "video", "audio", "tracks", "markers", "master", "rights"},
		"timebase":    {"ticks_per_second"},
		"video":       {"fps", "width", "height", "color", "background"},
		"color":       {"space", "range"},
		"fps":         {"num", "den"},
		"audio":       {"sample_rate", "channels"},
		"track":       {"id", "kind", "locked", "visible", "muted", "gain_db", "clips"},
		"clip":        {"id", "source", "at", "src_in", "src_out", "duration", "speed", "fit", "transform", "motion", "audio", "transition_in", "effects"},
		"source":      {"kind", "media_id", "asset_version_id", "sha256"},
		"effect":      {"id", "type", "enabled", "params", "meta"},
		"marker":      {"id", "at", "kind", "label"},
		"master":      {"effects", "audio"},
		"masterAudio": {"loudness"},
		"loudness":    {"target_lufs", "true_peak_db"},
		"rights":      {"music_declared", "declared_at"},
		"transition":  {"kind", "duration", "align"},
		"clipAudio":   {"gain_db", "fade_in", "fade_out", "mute"},
		"transform":   {"scale", "position", "rotate", "crop"},
		"motion":      {"kind", "from", "to", "easing"},
		"pose":        {"scale", "center"},
	}
	basicRanges = map[string][2]float64{"exposure_ev": {-5, 5}, "temperature": {-100, 100}, "tint": {-100, 100}, "contrast": {-100, 100},
		"highlights": {-100, 100}, "shadows": {-100, 100}, "whites": {-100, 100}, "blacks": {-100, 100}, "saturation": {-100, 100}, "vibrance": {-100, 100}}
)

func inR(v any, lo, hi float64) bool {
	f, ok := v.(float64)
	return isNum(v) && ok && f >= lo && f <= hi
}
func vec(v any, n int, lo, hi float64) bool {
	a, ok := v.([]any)
	if !ok || len(a) != n {
		return false
	}
	for _, x := range a {
		if !inR(x, lo, hi) {
			return false
		}
	}
	return true
}
func esc(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }

// Validate is FN-T-12: all violations (at most 100) in rule order; if any VT-01
// issue exists only VT-01 issues are returned.
func Validate(d Doc, ctx Context) []Issue {
	var out []Issue
	add := func(rule, ptr, code string) {
		if len(out) < maxIssues {
			out = append(out, Issue{rule, ptr, code})
		}
	}
	closed := func(v any, kind, p string) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			known := false
			for _, a := range closedKeys[kind] {
				known = known || a == k
			}
			if !known {
				add("VT-01", p+"/"+esc(k), "DOC_SCHEMA")
			}
		}
	}
	// VT-01
	closed(d, "root", "")
	if d["schema_version"] != float64(1) {
		add("VT-01", "/schema_version", "DOC_SCHEMA")
	}
	video, master := obj(d["video"]), obj(d["master"])
	closed(d["timebase"], "timebase", "/timebase")
	closed(video, "video", "/video")
	closed(video["color"], "color", "/video/color")
	closed(video["fps"], "fps", "/video/fps")
	closed(d["audio"], "audio", "/audio")
	closed(master, "master", "/master")
	closed(master["audio"], "masterAudio", "/master/audio")
	closed(obj(master["audio"])["loudness"], "loudness", "/master/audio/loudness")
	closed(d["rights"], "rights", "/rights")
	if f := obj(video["fps"]); f == nil || !supportedFPS[fmt.Sprintf("%v/%v", f["num"], f["den"])] {
		add("VT-01", "/video/fps", "DOC_SCHEMA")
	}
	tracks := arr(d["tracks"])
	for ti, tv := range tracks {
		closed(tv, "track", fmt.Sprintf("/tracks/%d", ti))
		for ci, cv := range arr(obj(tv)["clips"]) {
			p := fmt.Sprintf("/tracks/%d/clips/%d", ti, ci)
			c := obj(cv)
			closed(c, "clip", p)
			closed(c["source"], "source", p+"/source")
			closed(c["transition_in"], "transition", p+"/transition_in")
			closed(c["audio"], "clipAudio", p+"/audio")
			closed(c["transform"], "transform", p+"/transform")
			closed(c["motion"], "motion", p+"/motion")
			closed(obj(c["motion"])["from"], "pose", p+"/motion/from")
			closed(obj(c["motion"])["to"], "pose", p+"/motion/to")
			for _, k := range []string{"at", "src_in", "src_out", "duration"} {
				if v, has := c[k]; has && !isInt(v) {
					add("VT-01", p+"/"+k, "DOC_SCHEMA")
				}
			}
			for ei, e := range arr(c["effects"]) {
				closed(e, "effect", fmt.Sprintf("%s/effects/%d", p, ei))
			}
		}
	}
	for mi, mv := range arr(d["markers"]) {
		closed(mv, "marker", fmt.Sprintf("/markers/%d", mi))
		if v, has := obj(mv)["at"]; has && !isInt(v) {
			add("VT-01", fmt.Sprintf("/markers/%d/at", mi), "DOC_SCHEMA")
		}
	}
	if len(out) > 0 {
		return out
	}
	num, den := fpsOf(d)
	ft, _ := FrameTicks(num, den)
	clips := func(t any) []any { return arr(obj(t)["clips"]) }
	dur := func(c any) int64 { return ClipDuration(obj(c)) }
	// VT-02
	nv, na, nc, end := 0, 0, 0, int64(0)
	for _, t := range tracks {
		switch str(obj(t)["kind"]) {
		case "video":
			nv++
		case "audio":
			na++
		}
		for _, c := range clips(t) {
			nc++
			end = max(end, toInt(obj(c)["at"])+dur(c))
		}
	}
	if raw, err := Canonicalize(d); err != nil || len(raw) > MaxDocBytes || nv > 4 || na > 4 || nc > 2000 || end > MaxDurationTick {
		add("VT-02", "/tracks", "DOC_LIMIT")
	}
	// VT-03
	seen := map[string]bool{}
	chk := func(id any, p string) {
		s, ok := id.(string)
		if !ok || !idRE.MatchString(s) || seen[s] {
			add("VT-03", p, "DOC_ID_DUPLICATE")
		}
		if ok {
			seen[s] = true
		}
	}
	for ti, t := range tracks {
		chk(obj(t)["id"], fmt.Sprintf("/tracks/%d/id", ti))
		for ci, c := range clips(t) {
			chk(obj(c)["id"], fmt.Sprintf("/tracks/%d/clips/%d/id", ti, ci))
			for ei, e := range arr(obj(c)["effects"]) {
				chk(obj(e)["id"], fmt.Sprintf("/tracks/%d/clips/%d/effects/%d/id", ti, ci, ei))
			}
		}
	}
	for mi, m := range arr(d["markers"]) {
		chk(obj(m)["id"], fmt.Sprintf("/markers/%d/id", mi))
	}
	// VT-04
	for ti, t := range tracks {
		cs := clips(t)
		for ci := 1; ci < len(cs); ci++ {
			a, b := obj(cs[ci-1]), obj(cs[ci])
			if toInt(b["at"]) < toInt(a["at"]) || toInt(a["at"])+dur(a) > toInt(b["at"]) {
				add("VT-04", fmt.Sprintf("/tracks/%d/clips/%d/at", ti, ci), "CLIP_OVERLAP")
			}
		}
	}
	// VT-05
	for ti, t := range tracks {
		for ci, cv := range clips(t) {
			c := obj(cv)
			s := obj(c["source"])
			if str(s["kind"]) == "photo" {
				continue
			}
			p := fmt.Sprintf("/tracks/%d/clips/%d", ti, ci)
			si, so := toInt(c["src_in"]), toInt(c["src_out"])
			switch {
			case si < 0:
				add("VT-05", p+"/src_in", "CLIP_RANGE")
			case so <= si:
				add("VT-05", p+"/src_out", "CLIP_RANGE")
			default:
				if sd, ok := ctx.SourceDurations[str(s["media_id"])]; ok && so > sd {
					add("VT-05", p+"/src_out", "CLIP_RANGE")
				}
			}
		}
	}
	// VT-06
	for ti, t := range tracks {
		for ci, cv := range clips(t) {
			s := obj(obj(cv)["source"])
			sid := str(s["media_id"])
			if sid == "" {
				sid = str(s["asset_version_id"])
			}
			p := fmt.Sprintf("/tracks/%d/clips/%d/source", ti, ci)
			if ctx.SourceStates[sid] != "ready" {
				add("VT-06", p, "SOURCE_NOT_READY")
			} else if ctx.SourceHashes[sid] != str(s["sha256"]) {
				add("VT-06", p, "SOURCE_HASH_MISMATCH")
			}
		}
	}
	// VT-07
	byID := map[string]map[string]any{}
	for _, t := range tracks {
		byID[str(obj(t)["id"])] = obj(t)
	}
	for ti, t := range tracks {
		for ci, cv := range clips(t) {
			c := obj(cv)
			p := fmt.Sprintf("/tracks/%d/clips/%d", ti, ci)
			bad := func(sub string) { add("VT-07", p+"/"+sub, "EFFECT_PARAM") }
			if f, has := c["fit"]; has && f != "fit" && f != "fill" && f != "none" {
				bad("fit")
			}
			if tf := obj(c["transform"]); tf != nil {
				if !inR(tf["scale"], 0.1, 8) {
					bad("transform/scale")
				}
				if !vec(tf["position"], 2, -1, 1) {
					bad("transform/position")
				}
				if !inR(tf["rotate"], -180, 180) {
					bad("transform/rotate")
				}
				if !vec(tf["crop"], 4, 0, 0.45) {
					bad("transform/crop")
				}
			}
			if au := obj(c["audio"]); au != nil {
				if !inR(au["gain_db"], -60, 24) {
					bad("audio/gain_db")
				}
				for _, k := range []string{"fade_in", "fade_out"} {
					if !isInt(au[k]) || toInt(au[k]) < 0 || toInt(au[k])%ft != 0 {
						bad("audio/" + k)
					}
				}
				if isInt(au["fade_in"]) && isInt(au["fade_out"]) && toInt(au["fade_in"])+toInt(au["fade_out"]) > ClipDuration(c) {
					bad("audio/fade_out")
				}
				if _, ok := au["mute"].(bool); !ok {
					bad("audio/mute")
				}
			}
			if m := obj(c["motion"]); m != nil {
				if m["kind"] != "pan_zoom" {
					bad("motion/kind")
				}
				if m["easing"] != "linear" && m["easing"] != "ease_in_out" {
					bad("motion/easing")
				}
				for _, k := range []string{"from", "to"} {
					pose := obj(m[k])
					if pose == nil || !inR(pose["scale"], 1, 4) || !vec(pose["center"], 2, 0, 1) {
						bad("motion/" + k)
					}
				}
			}
			if str(obj(c["source"])["kind"]) == "photo" && (!isInt(c["duration"]) || toInt(c["duration"]) < ft || toInt(c["duration"])%ft != 0) {
				bad("duration")
			}
			for ei, ev := range arr(c["effects"]) {
				validateEffect(obj(ev), fmt.Sprintf("%s/effects/%d/params", p, ei), add)
			}
		}
	}
	// VT-08
	for ti, t := range tracks {
		cs := clips(t)
		for ci, cv := range cs {
			tr := obj(obj(cv)["transition_in"])
			if tr == nil {
				continue
			}
			p := fmt.Sprintf("/tracks/%d/clips/%d/transition_in", ti, ci)
			if ci == 0 && tr["kind"] != "fade_in" && tr["kind"] != "cut" {
				add("VT-08", p+"/kind", "TRANSITION_INVALID")
				continue
			}
			lim := dur(cv) / 2
			if ci > 0 {
				lim = min(dur(cv), dur(cs[ci-1])) / 2
			}
			if toInt(tr["duration"]) > lim {
				add("VT-08", p+"/duration", "TRANSITION_INVALID")
			}
		}
	}
	// VT-09
	for ti, t := range tracks {
		for ci, cv := range clips(t) {
			for ei, ev := range arr(obj(cv)["effects"]) {
				e := obj(ev)
				if e["type"] != "audio.duck" {
					continue
				}
				sc, ok := obj(e["params"])["sidechain_tracks"].([]any)
				if !ok {
					continue
				}
				for _, s := range sc {
					o := byID[str(s)]
					if o == nil || o["kind"] != "audio" || str(s) == str(obj(t)["id"]) {
						add("VT-09", fmt.Sprintf("/tracks/%d/clips/%d/effects/%d/params/sidechain_tracks", ti, ci, ei), "SIDECHAIN_INVALID")
						break
					}
				}
			}
		}
	}
	// VT-10
	if ctx.ForRender && len(ctx.MusicClipIDs) > 0 && obj(d["rights"])["music_declared"] != true {
		add("VT-10", "/rights/music_declared", "RIGHTS_REQUIRED")
	}
	return out
}

func validateEffect(e map[string]any, ep string, add func(rule, ptr, code string)) {
	params := obj(e["params"])
	if params == nil {
		params = map[string]any{}
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	badp := func(k string) { add("VT-07", ep+"/"+esc(k), "EFFECT_PARAM") }
	only := func(allowed ...string) {
		for _, k := range keys {
			ok := false
			for _, a := range allowed {
				ok = ok || a == k
			}
			if !ok {
				badp(k)
			}
		}
	}
	switch e["type"] {
	case "color.basic":
		for _, k := range keys {
			if r, ok := basicRanges[k]; !ok || !inR(params[k], r[0], r[1]) {
				badp(k)
			}
		}
	case "color.cdl":
		only("slope", "offset", "power", "saturation")
		for _, s := range []struct {
			k      string
			n      int
			lo, hi float64
		}{{"slope", 3, 0, 4}, {"offset", 3, -1, 1}, {"power", 3, 0.1, 4}, {"saturation", 0, 0, 4}} {
			v, has := params[s.k]
			if !has || (s.n > 0 && !vec(v, s.n, s.lo, s.hi)) || (s.n == 0 && !inR(v, s.lo, s.hi)) {
				badp(s.k)
			}
		}
	case "color.curves":
		if len(keys) == 0 {
			badp("")
		}
		for _, k := range keys {
			pts, ok := params[k].([]any)
			good := ok && (k == "master" || k == "r" || k == "g" || k == "b") && len(pts) >= 2 && len(pts) <= 16
			for i := 0; good && i < len(pts); i++ {
				good = vec(pts[i], 2, 0, 1) && (i == 0 || arr(pts[i])[0].(float64) > arr(pts[i-1])[0].(float64))
			}
			if !good {
				badp(k)
			}
		}
	case "color.lut3d":
		l := obj(params["lut"])
		if l == nil || l["kind"] != "lut" || str(l["media_id"]) == "" || str(l["sha256"]) == "" {
			badp("lut")
		}
		if !inR(params["intensity"], 0, 1) {
			badp("intensity")
		}
		only("lut", "intensity")
	case "audio.gain":
		if !inR(params["gain_db"], -60, 24) {
			badp("gain_db")
		}
		only("gain_db")
	case "audio.fade":
		for _, k := range []string{"fade_in", "fade_out"} {
			if !isInt(params[k]) || toInt(params[k]) < 0 {
				badp(k)
			}
		}
		only("fade_in", "fade_out")
	case "audio.duck":
		sc, ok := params["sidechain_tracks"].([]any)
		uniq := map[string]bool{}
		for _, s := range sc {
			uniq[str(s)] = true
		}
		if !ok || len(sc) < 1 || len(sc) > 4 || len(uniq) != len(sc) {
			badp("sidechain_tracks")
		}
		for _, s := range []struct {
			k      string
			lo, hi float64
		}{{"threshold_db", -60, 0}, {"depth_db", 0, 40}, {"attack_ms", 1, 1000}, {"release_ms", 10, 5000}} {
			if !inR(params[s.k], s.lo, s.hi) {
				badp(s.k)
			}
		}
		only("sidechain_tracks", "threshold_db", "depth_db", "attack_ms", "release_ms")
	}
}
