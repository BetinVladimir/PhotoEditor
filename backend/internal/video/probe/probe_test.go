package probe

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

const sample = `{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"r_frame_rate":"30000/1001","pix_fmt":"yuv420p"},
{"codec_type":"audio","codec_name":"aac","channels":1,"sample_rate":"48000"}],"format":{"format_name":"mov,mp4","duration":"2.500000"}}`

func TestParseJSON(t *testing.T) {
	f, err := ParseJSON([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if f.DurationTicks != 1764000000 || f.Width != 1280 || f.FPSNum != 30000 || f.FPSDen != 1001 || !f.HasAudio || f.Channels != 1 || f.SampleRate != 48000 {
		t.Fatalf("%+v", f)
	}
}

func TestParseJSONRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want error
	}{
		"garbage":   {"nope", ErrUnreadable},
		"nostreams": {`{"streams":[],"format":{"duration":"1"}}`, ErrNoStreams},
		"nodur":     {`{"streams":[{"codec_type":"audio","channels":2}],"format":{}}`, ErrUnreadable},
		"toolong":   {`{"streams":[{"codec_type":"audio","channels":2}],"format":{"duration":"20000"}}`, ErrTooLong},
	} {
		if _, err := ParseJSON([]byte(tc.in)); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", name, err, tc.want)
		}
	}
}

func TestRunRealFFprobe(t *testing.T) {
	ff, e1 := exec.LookPath("ffmpeg")
	fp, e2 := exec.LookPath("ffprobe")
	if e1 != nil || e2 != nil {
		t.Skip("ffmpeg not installed")
	}
	p := filepath.Join(t.TempDir(), "a.mp4")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-ac", "1", p).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	f, _, err := Run(context.Background(), fp, p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Width != 320 || f.Height != 240 || f.FPSNum != 25 || !f.HasAudio || f.Channels != 1 || f.DurationTicks < 1340640000 || f.DurationTicks > 1481760000 {
		t.Fatalf("%+v", f)
	}
}
