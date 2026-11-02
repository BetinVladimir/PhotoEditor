package timeline

import (
	"errors"
	"math/big"
	"sort"
)

// EditError is a command failure code (DS-10); it is returned as the error text.
type EditError string

func (e EditError) Error() string { return string(e) }

// Applied records one split made by ApplyCuts.
type Applied struct {
	Boundary string `json:"boundary"`
	Clip     string `json:"clip"`
	New      string `json:"new"`
	At       int64  `json:"at"`
}

func speedOf(c map[string]any) (int64, int64) {
	if sp := obj(c["speed"]); sp != nil {
		return toInt(sp["num"]), toInt(sp["den"])
	}
	return 1, 1
}

// mulDiv returns a*b/c and whether the division was exact (arbitrary precision).
func mulDiv(a, b, c int64) (int64, bool) {
	p := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	q, r := new(big.Int).QuoRem(p, big.NewInt(c), new(big.Int))
	return q.Int64(), r.Sign() == 0
}

type found struct {
	track map[string]any
	idx   int
	clip  map[string]any
}

func find(d Doc, id string) (found, bool) {
	for _, tv := range arr(d["tracks"]) {
		t := obj(tv)
		for i, cv := range arr(t["clips"]) {
			if c := obj(cv); str(c["id"]) == id {
				return found{t, i, c}, true
			}
		}
	}
	return found{}, false
}

func trackClips(t map[string]any) []any { return arr(t["clips"]) }

func frameTicksOf(d Doc) int64 {
	n, den := fpsOf(d)
	ft, _ := FrameTicks(n, den)
	return ft
}

// SplitClip is FN-T-05.
func SplitClip(src Doc, clipID string, at int64, newID string) (Doc, error) {
	d := clone(src)
	f, ok := find(d, clipID)
	if !ok {
		return nil, EditError("CLIP_NOT_FOUND")
	}
	c := f.clip
	if str(obj(c["source"])["kind"]) == "photo" {
		return nil, EditError("SPLIT_PHOTO_UNSUPPORTED")
	}
	cAt := toInt(c["at"])
	if !(cAt < at && at < cAt+ClipDuration(c)) {
		return nil, EditError("SPLIT_OUT_OF_RANGE")
	}
	if at%frameTicksOf(d) != 0 {
		return nil, EditError("SPLIT_NOT_ALIGNED")
	}
	n, den := speedOf(c)
	so, exact := mulDiv(at-cAt, n, den)
	if !exact {
		return nil, EditError("SPLIT_NOT_ALIGNED")
	}
	b := clone(map[string]any{"c": c})["c"].(map[string]any)
	srcIn := toInt(c["src_in"])
	b["id"], b["at"], b["src_in"] = newID, float64(at), float64(srcIn+so)
	c["src_out"] = float64(srcIn + so)
	delete(b, "transition_in")
	if au := obj(c["audio"]); au != nil {
		au["fade_out"] = float64(0)
		obj(b["audio"])["fade_in"] = float64(0)
	}
	for k, e := range arr(b["effects"]) {
		obj(e)["id"] = newID + "-e" + itoa(k+1)
	}
	clips := trackClips(f.track)
	f.track["clips"] = append(clips[:f.idx+1], append([]any{b}, clips[f.idx+1:]...)...)
	return d, nil
}

// TrimEdge is FN-T-06; edge is "in" or "out".
func TrimEdge(src Doc, clipID, edge string, pos, sourceDuration int64) (Doc, error) {
	d := clone(src)
	f, ok := find(d, clipID)
	if !ok {
		return nil, EditError("CLIP_NOT_FOUND")
	}
	if pos%frameTicksOf(d) != 0 {
		return nil, EditError("TRIM_NOT_ALIGNED")
	}
	c, clips := f.clip, trackClips(f.track)
	n, den := speedOf(c)
	cAt := toInt(c["at"])
	if edge == "in" {
		prevEnd := int64(0)
		if f.idx > 0 {
			p := obj(clips[f.idx-1])
			prevEnd = toInt(p["at"]) + ClipDuration(p)
		}
		if pos < prevEnd {
			return nil, EditError("TRIM_OVERLAP")
		}
		if pos >= cAt+ClipDuration(c) {
			return nil, EditError("TRIM_EMPTY")
		}
		delta, exact := mulDiv(pos-cAt, n, den)
		if !exact {
			return nil, EditError("TRIM_NOT_ALIGNED")
		}
		ns := toInt(c["src_in"]) + delta
		if ns < 0 {
			return nil, EditError("TRIM_BEFORE_SOURCE")
		}
		c["src_in"], c["at"] = float64(ns), float64(pos)
		return d, nil
	}
	if pos <= cAt {
		return nil, EditError("TRIM_EMPTY")
	}
	if f.idx+1 < len(clips) && pos > toInt(obj(clips[f.idx+1])["at"]) {
		return nil, EditError("TRIM_OVERLAP")
	}
	delta, exact := mulDiv(pos-cAt, n, den)
	if !exact {
		return nil, EditError("TRIM_NOT_ALIGNED")
	}
	no := toInt(c["src_in"]) + delta
	if no > sourceDuration {
		return nil, EditError("TRIM_BEYOND_SOURCE")
	}
	c["src_out"] = float64(no)
	return d, nil
}

// MoveClip is FN-T-07; trackID may be empty for the clip's own track.
func MoveClip(src Doc, clipID string, newAt int64, trackID string) (Doc, error) {
	d := clone(src)
	f, ok := find(d, clipID)
	if !ok {
		return nil, EditError("CLIP_NOT_FOUND")
	}
	if newAt < 0 || newAt%frameTicksOf(d) != 0 {
		return nil, EditError("MOVE_NOT_ALIGNED")
	}
	tgt := f.track
	if trackID != "" {
		tgt = nil
		for _, tv := range arr(d["tracks"]) {
			if str(obj(tv)["id"]) == trackID {
				tgt = obj(tv)
			}
		}
		if tgt == nil {
			return nil, EditError("TRACK_NOT_FOUND")
		}
	}
	kind := "video"
	if str(obj(f.clip["source"])["kind"]) == "audio" {
		kind = "audio"
	}
	if str(tgt["kind"]) != kind {
		return nil, EditError("MOVE_TRACK_KIND")
	}
	clips := trackClips(f.track)
	f.track["clips"] = append(clips[:f.idx:f.idx], clips[f.idx+1:]...)
	dur := ClipDuration(f.clip)
	for _, ov := range trackClips(tgt) {
		o := obj(ov)
		if newAt < toInt(o["at"])+ClipDuration(o) && toInt(o["at"]) < newAt+dur {
			return nil, EditError("MOVE_OVERLAP")
		}
	}
	f.clip["at"] = float64(newAt)
	list := append(trackClips(tgt), f.clip)
	sort.SliceStable(list, func(i, j int) bool { return toInt(obj(list[i])["at"]) < toInt(obj(list[j])["at"]) })
	tgt["clips"] = list
	return d, nil
}

// DeleteClip is FN-T-08.
func DeleteClip(src Doc, clipID string, ripple bool) (Doc, error) {
	d := clone(src)
	f, ok := find(d, clipID)
	if !ok {
		return nil, EditError("CLIP_NOT_FOUND")
	}
	dur, at := ClipDuration(f.clip), toInt(f.clip["at"])
	clips := trackClips(f.track)
	rest := append(clips[:f.idx:f.idx], clips[f.idx+1:]...)
	if ripple {
		for _, ov := range rest {
			if o := obj(ov); toInt(o["at"]) > at {
				o["at"] = float64(toInt(o["at"]) - dur)
			}
		}
	}
	f.track["clips"] = rest
	return d, nil
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// SetSpeed is FN-T-09.
func SetSpeed(src Doc, clipID string, num, den int64) (Doc, error) {
	d := clone(src)
	f, ok := find(d, clipID)
	if !ok {
		return nil, EditError("CLIP_NOT_FOUND")
	}
	c := f.clip
	if str(obj(c["source"])["kind"]) == "photo" {
		return nil, EditError("SPEED_PHOTO")
	}
	if num <= 0 || den <= 0 || num*4 < den || num > 4*den {
		return nil, EditError("SPEED_RANGE")
	}
	g := gcd(num, den)
	num, den = num/g, den/g
	ft := frameTicksOf(d)
	raw, _ := mulDiv(toInt(c["src_out"])-toInt(c["src_in"]), den, num)
	dur := raw / ft * ft
	if dur <= 0 {
		return nil, EditError("SPEED_TOO_SHORT")
	}
	clips := trackClips(f.track)
	if f.idx+1 < len(clips) && toInt(c["at"])+dur > toInt(obj(clips[f.idx+1])["at"]) {
		return nil, EditError("SPEED_OVERLAP")
	}
	so, _ := mulDiv(dur, num, den)
	c["src_out"] = float64(toInt(c["src_in"]) + so)
	c["speed"] = map[string]any{"num": float64(num), "den": float64(den)}
	return d, nil
}

// Boundary is the part of a cut report used by ApplyCuts.
type Boundary struct {
	ID         string
	StartTicks int64
	EndTicks   int64
}

// ErrIDsExhausted is returned when newIDs runs out.
var ErrIDsExhausted = errors.New("APPLY_IDS_EXHAUSTED")

// ApplyCuts is FN-T-10; mode is split_at_start, split_at_middle or markers_only.
func ApplyCuts(src Doc, mediaID string, boundaries []Boundary, accepted []string, mode string, newIDs []string) (Doc, []Applied, error) {
	d := clone(src)
	ft := frameTicksOf(d)
	acc := map[string]bool{}
	for _, a := range accepted {
		acc[a] = true
	}
	var picked []Boundary
	for _, b := range boundaries {
		if acc[b.ID] {
			picked = append(picked, b)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].StartTicks > picked[j].StartTicks })
	applied := []Applied{}
	next := 0
	take := func() (string, bool) {
		if next >= len(newIDs) {
			return "", false
		}
		next++
		return newIDs[next-1], true
	}
	for _, b := range picked {
		ts := b.StartTicks
		if mode == "split_at_middle" {
			ts = (b.StartTicks + b.EndTicks) / 2
		}
		for _, tv := range arr(d["tracks"]) {
			t := obj(tv)
			if str(t["kind"]) != "video" {
				continue
			}
			snapshot := append([]any(nil), trackClips(t)...)
			for _, cv := range snapshot {
				c := obj(cv)
				if str(obj(c["source"])["media_id"]) != mediaID {
					continue
				}
				si, so := toInt(c["src_in"]), toInt(c["src_out"])
				if !(si < ts && ts < so) {
					continue
				}
				n, den := speedOf(c)
				off, _ := mulDiv(ts-si, den, n)
				tl := toInt(c["at"]) + off
				tl = (tl + ft/2) / ft * ft
				if !(toInt(c["at"]) < tl && tl < toInt(c["at"])+ClipDuration(c)) {
					continue
				}
				if mode == "markers_only" {
					dup := false
					for _, mv := range arr(d["markers"]) {
						m := obj(mv)
						dup = dup || (toInt(m["at"]) == tl && str(m["kind"]) == "cut")
					}
					if dup {
						continue
					}
					nid, ok := take()
					if !ok {
						return nil, nil, ErrIDsExhausted
					}
					ms := append(arr(d["markers"]), map[string]any{"id": nid, "at": float64(tl), "kind": "cut", "label": ""})
					sort.SliceStable(ms, func(i, j int) bool { return toInt(obj(ms[i])["at"]) < toInt(obj(ms[j])["at"]) })
					d["markers"] = ms
					applied = append(applied, Applied{b.ID, str(c["id"]), nid, tl})
					continue
				}
				nid, ok := take()
				if !ok {
					return nil, nil, ErrIDsExhausted
				}
				nd, err := SplitClip(d, str(c["id"]), tl, nid)
				if err != nil {
					return nil, nil, err
				}
				d = nd
				applied = append(applied, Applied{b.ID, str(c["id"]), nid, tl})
				// the track slice in the old tree is stale; re-resolve the track in the new tree
				for _, ntv := range arr(d["tracks"]) {
					if str(obj(ntv)["id"]) == str(t["id"]) {
						t = obj(ntv)
					}
				}
			}
		}
	}
	return d, applied, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}
