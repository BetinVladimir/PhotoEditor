package render

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/probe"
	"photoeditor/backend/internal/video/testvectors"
)

func TestFmtSeconds(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 705600000: "1", 1764000000: "2.5", 1: "0.000000001", 7056000000: "10"} {
		if got := FmtSeconds(in); got != want {
			t.Errorf("FmtSeconds(%d)=%q want %q", in, got, want)
		}
	}
	if SamplesAt(705600000) != 48000 || SamplesAt(1764000000) != 120000 {
		t.Error("SamplesAt")
	}
}

func doc(t *testing.T, s string) Doc {
	var d Doc
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

const twoClips = `{"video":{"fps":{"num":25,"den":1},"width":320,"height":240},"tracks":[
{"id":"V1","kind":"video","clips":[
 {"id":"c1","source":{"kind":"video","media_id":"m1"},"at":0,"src_in":0,"src_out":705600000,"fit":"fit","effects":[]},
 {"id":"c2","source":{"kind":"video","media_id":"m1"},"at":705600000,"src_in":352800000,"src_out":1058400000,"fit":"fill","audio":{"gain_db":-6,"mute":false},"effects":[]}]}]}`

func TestCompileRejections(t *testing.T) {
	src := map[string]Source{"m1": {DurationTicks: 1411200000, HasVideo: true, HasAudio: true, Channels: 1}}
	cases := map[string]string{
		"COMPILE_TRANSITION_UNSUPPORTED": `"transition_in":{"kind":"wipe","duration":1},`,
		"COMPILE_SPEED_UNSUPPORTED":      `"speed":{"num":5,"den":1},`,
		"COMPILE_TRANSFORM_UNSUPPORTED":  `"transform":{"scale":1},`,
	}
	for code, frag := range cases {
		d := doc(t, strings.Replace(twoClips, `"id":"c1",`, `"id":"c1",`+frag, 1))
		_, err := Compile(d, src)
		var ce Error
		if !errors.As(err, &ce) || ce.Code != code || ce.Clip != "c1" {
			t.Errorf("%s: got %v", code, err)
		}
	}
	if _, err := Compile(doc(t, twoClips), map[string]Source{}); err == nil || !strings.Contains(err.Error(), "COMPILE_SOURCE_MISSING:c1") {
		t.Errorf("missing source: %v", err)
	}
	if _, err := Compile(doc(t, `{"video":{"fps":{"num":25,"den":1},"width":320,"height":240},"tracks":[]}`), src); err == nil || err.Error() != "COMPILE_EMPTY" {
		t.Errorf("empty: %v", err)
	}
}

func TestCompileDeterministic(t *testing.T) {
	src := map[string]Source{"m1": {HasVideo: true, HasAudio: true, Channels: 1}}
	a, err := Compile(doc(t, twoClips), src)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Compile(doc(t, twoClips), src)
	if a.FilterComplex != b.FilterComplex || a.DurationTicks != 1411200000 || len(a.Inputs) != 2 {
		t.Fatalf("%+v", a)
	}
	if !strings.Contains(a.FilterComplex, "adelay=0S:all=1") || !strings.Contains(a.FilterComplex, "adelay=48000S:all=1") || !strings.Contains(a.FilterComplex, "volume=-6dB") {
		t.Fatal(a.FilterComplex)
	}
}

// Real ffmpeg: the compiled command must produce a file with the document's
// size and duration and an audio stream.
func TestRenderWithRealFFmpeg(t *testing.T) {
	ff, e1 := exec.LookPath("ffmpeg")
	fp, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.mp4")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "1", srcPath).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	f, _, err := probe.Run(t.Context(), fp, srcPath)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(doc(t, twoClips), map[string]Source{"m1": {DurationTicks: f.DurationTicks, HasVideo: f.HasVideo, HasAudio: f.HasAudio, Channels: f.Channels}})
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "out.mp4")
	args, err := Args(plan, map[string]string{"m1": srcPath}, outPath, "libx264", 25, 1)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), ff, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s\n%s", err, out, plan.FilterComplex)
	}
	got, _, err := probe.Run(t.Context(), fp, outPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 320 || got.Height != 240 || !got.HasAudio || got.Channels != 2 || got.SampleRate != 48000 || got.FPSNum != 25 {
		t.Fatalf("%+v", got)
	}
	if d := got.DurationTicks - plan.DurationTicks; d < -35280000 || d > 70560000 {
		t.Fatalf("duration %d want %d", got.DurationTicks, plan.DurationTicks)
	}
}

func TestAtempoChainVectors(t *testing.T) {
	var v struct {
		AtempoChain []struct {
			Num, Den int64
			Out      []string
		} `json:"atempo_chain"`
	}
	testvectors.Load(t, "compile", &v)
	if len(v.AtempoChain) != 8 {
		t.Fatalf("vectors: %d", len(v.AtempoChain))
	}
	for _, c := range v.AtempoChain {
		got := AtempoChain(c.Num, c.Den)
		if strings.Join(got, ",") != strings.Join(c.Out, ",") {
			t.Errorf("%d/%d: got %v want %v", c.Num, c.Den, got, c.Out)
		}
	}
}

// FN-E-02 vectors ET-01..ET-10 shared with the editor.
func TestEffectiveTransitionVectors(t *testing.T) {
	var v struct {
		Durations map[string]int64          `json:"source_duration_ticks"`
		Docs      map[string]map[string]any `json:"docs"`
		Cases     []struct {
			ID, Doc, Clip string
			Expect        *struct {
				Kind                            string
				Start, End, Duration, Requested int64
				Shortened                       bool
			}
		} `json:"effective_transition"`
	}
	testvectors.Load(t, "editor", &v)
	if len(v.Cases) != 10 {
		t.Fatalf("vectors: %d", len(v.Cases))
	}
	sources := map[string]Source{}
	for k, d := range v.Durations {
		sources[k] = Source{DurationTicks: d, HasVideo: true}
	}
	for _, c := range v.Cases {
		for _, tr := range arr(v.Docs[c.Doc]["tracks"]) {
			clips := arr(obj(tr)["clips"])
			for i, cl := range clips {
				if str(obj(cl)["id"]) != c.Clip {
					continue
				}
				got := effectiveTransition(clips, i, sources)
				if c.Expect == nil {
					if got.kind != "cut" || got.shortened {
						t.Errorf("%s: want null, got %+v", c.ID, got)
					}
					continue
				}
				w := c.Expect
				if got.kind != w.Kind || got.start != w.Start || got.end != w.End || got.dur != w.Duration || got.requested != w.Requested || got.shortened != w.Shortened {
					t.Errorf("%s: got %+v want %+v", c.ID, got, *w)
				}
			}
		}
	}
}

const speedFade = `{"video":{"fps":{"num":25,"den":1},"width":320,"height":240},"tracks":[
{"id":"V1","kind":"video","clips":[
 {"id":"c1","source":{"kind":"video","media_id":"m1"},"at":0,"src_in":0,"src_out":1411200000,"speed":{"num":2,"den":1},"fit":"fit","effects":[]},
 {"id":"c2","source":{"kind":"video","media_id":"m1"},"at":705600000,"src_in":705600000,"src_out":1411200000,"fit":"fit","transition_in":{"kind":"%s","duration":352800000},"effects":[]}]}]}`

func TestCompileSpeedAndTransitionGraph(t *testing.T) {
	src := map[string]Source{"m1": {DurationTicks: 2822400000, HasVideo: true, HasAudio: true, Channels: 2}}
	p, err := Compile(doc(t, strings.Replace(speedFade, "%s", "crossfade", 1)), src)
	if err != nil {
		t.Fatal(err)
	}
	g := p.FilterComplex
	for _, want := range []string{"setpts=(PTS-STARTPTS)*1/2", "round=near", "atempo=2", "fade=t=in:st=0:d=0.5:alpha=1", "afade=t=in:st=0:d=0.5:curve=tri", "afade=t=out:st=0.75:d=0.5:curve=tri"} {
		if !strings.Contains(g, want) {
			t.Errorf("missing %q in\n%s", want, g)
		}
	}
	// c1 lasts 1 s on the timeline (2 s of source at x2), c2 starts at 1 s,
	// and the centred crossfade borrows 0.25 s from both sides.
	if p.DurationTicks != 705600000*2 {
		t.Errorf("duration %d", p.DurationTicks)
	}
	if o := p.Inputs[0].Options; o[len(o)-1] != "2.5" {
		t.Errorf("c1 reads %v, want 2.5 s of source (1.25 s of timeline at x2)", o)
	}
	d, err := Compile(doc(t, strings.Replace(speedFade, "%s", "dip_to_black", 1)), src)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(d.FilterComplex, "scale=in_range=tv:out_range=pc,fade="); n != 2 {
		t.Errorf("dip fades: %d", n)
	}
}

// Real ffmpeg: speed halves the duration, crossfade and dip produce the
// document length; the dip reaches broadcast black (Y=16, D-41).
func TestRenderSpeedAndTransitionsWithRealFFmpeg(t *testing.T) {
	ff, e1 := exec.LookPath("ffmpeg")
	fp, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.mp4")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "color=c=white:s=640x360:r=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "4", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", srcPath).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	f, _, err := probe.Run(t.Context(), fp, srcPath)
	if err != nil {
		t.Fatal(err)
	}
	src := map[string]Source{"m1": {DurationTicks: f.DurationTicks, HasVideo: true, HasAudio: true, Channels: f.Channels}}
	for _, kind := range []string{"crossfade", "dip_to_black"} {
		plan, err := Compile(doc(t, strings.Replace(speedFade, "%s", kind, 1)), src)
		if err != nil {
			t.Fatal(err)
		}
		outPath := filepath.Join(dir, kind+".mp4")
		args, _ := Args(plan, map[string]string{"m1": srcPath}, outPath, "libx264", 25, 1)
		if out, err := exec.CommandContext(t.Context(), ff, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s\n%s", kind, err, out, plan.FilterComplex)
		}
		got, _, err := probe.Run(t.Context(), fp, outPath)
		if err != nil {
			t.Fatal(err)
		}
		if d := got.DurationTicks - plan.DurationTicks; d < -35280000 || d > 70560000 || !got.HasAudio {
			t.Fatalf("%s: duration %d want %d audio=%v", kind, got.DurationTicks, plan.DurationTicks, got.HasAudio)
		}
		if kind != "dip_to_black" {
			continue
		}
		// Middle of the dip is at 1 s: first byte of a yuv420p crop is Y (gray would be full range).
		raw, err := exec.CommandContext(t.Context(), ff, "-v", "error", "-ss", "1", "-i", outPath, "-frames:v", "1", "-vf", "crop=2:2:100:100,format=yuv420p", "-f", "rawvideo", "-").Output()
		if err != nil || len(raw) < 1 {
			t.Fatalf("pixel: %v %v", err, raw)
		}
		if raw[0] < 14 || raw[0] > 20 {
			t.Errorf("dip luma %d, want about 16", raw[0])
		}
	}
}
