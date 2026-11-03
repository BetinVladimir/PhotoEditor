package render

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/probe"
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
		"COMPILE_TRANSITION_UNSUPPORTED": `"transition_in":{"kind":"crossfade","duration":1},`,
		"COMPILE_SPEED_UNSUPPORTED":      `"speed":{"num":2,"den":1},`,
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
