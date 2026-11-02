package timeline

import (
	"strconv"
	"strings"
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

type timeVector struct {
	FrameTicks   map[string]int64 `json:"frame_ticks"`
	SampleTicks  map[string]int64 `json:"sample_ticks"`
	Max3h        int64            `json:"max_ticks_3h"`
	FramesToTick []struct {
		FPS   string `json:"fps"`
		Frame int64  `json:"frame"`
		Ticks int64  `json:"ticks"`
	} `json:"frames_to_ticks"`
	TicksToFrame []struct {
		FPS   string `json:"fps"`
		Ticks int64  `json:"ticks"`
		Floor int64  `json:"floor"`
		Round int64  `json:"round"`
	} `json:"ticks_to_frame"`
}

func fps(t *testing.T, s string) (int64, int64) {
	t.Helper()
	p := strings.Split(s, "/")
	n, _ := strconv.ParseInt(p[0], 10, 64)
	d, _ := strconv.ParseInt(p[1], 10, 64)
	return n, d
}

func TestTimeVectors(t *testing.T) {
	var v timeVector
	testvectors.Load(t, "time", &v)
	for k, want := range v.FrameTicks {
		n, d := fps(t, k)
		if got, err := FrameTicks(n, d); err != nil || got != want {
			t.Fatalf("FrameTicks(%s) = %d, %v; want %d", k, got, err, want)
		}
	}
	for k, want := range v.SampleTicks {
		r, _ := strconv.ParseInt(k, 10, 64)
		if TicksPerSecond/r != want {
			t.Fatalf("sample ticks %s", k)
		}
	}
	if 3*3600*TicksPerSecond != v.Max3h {
		t.Fatal("max_ticks_3h")
	}
	for _, c := range v.FramesToTick {
		n, d := fps(t, c.FPS)
		if got, err := FramesToTicks(n, d, c.Frame); err != nil || got != c.Ticks {
			t.Fatalf("FramesToTicks %v: %d %v", c, got, err)
		}
	}
	for _, c := range v.TicksToFrame {
		n, d := fps(t, c.FPS)
		f, _ := TicksToFrame(n, d, c.Ticks, false)
		r, _ := TicksToFrame(n, d, c.Ticks, true)
		if f != c.Floor || r != c.Round {
			t.Fatalf("TicksToFrame %v: floor %d round %d", c, f, r)
		}
	}
}

func TestTimeErrors(t *testing.T) {
	// card TK-201 names 7/1, but 705600000 % 7 == 0; 11/1 is a rate that really does not divide the timebase
	if _, err := FrameTicks(11, 1); err != ErrFPSUnsupported {
		t.Fatalf("11/1: %v", err)
	}
	if _, err := FrameTicks(0, 1); err != ErrFPSUnsupported {
		t.Fatalf("0/1: %v", err)
	}
	if _, err := FramesToTicks(25, 1, -1); err != ErrFrameNegative {
		t.Fatalf("negative frame: %v", err)
	}
	if _, err := TicksToFrame(25, 1, -1, false); err != ErrTicksNegative {
		t.Fatalf("negative ticks: %v", err)
	}
	if _, err := FramesToTicks(25, 1, 1<<62); err == nil {
		t.Fatal("overflow must be reported")
	}
}
