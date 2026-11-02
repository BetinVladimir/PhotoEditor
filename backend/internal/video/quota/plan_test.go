package quota

import (
	"testing"

	"photoeditor/backend/internal/video/testvectors"
)

func TestQuotaVectors(t *testing.T) {
	var v struct {
		PartBytes []struct {
			Bytes      int64 `json:"bytes"`
			PartBytes  int64 `json:"part_bytes"`
			PartsTotal int64 `json:"parts_total"`
		} `json:"part_bytes"`
		Derivative []struct {
			Bytes    int64 `json:"bytes"`
			Estimate int64 `json:"estimate"`
		} `json:"derivative_estimate"`
		Render []struct {
			Duration int64 `json:"duration_ticks"`
			Maxrate  int64 `json:"maxrate_bps"`
			Audio    int64 `json:"audio_bps"`
			Estimate int64 `json:"estimate"`
		} `json:"render_estimate"`
		Admit []struct {
			Used     int64 `json:"used"`
			Reserved int64 `json:"reserved"`
			Limit    int64 `json:"limit"`
			Request  int64 `json:"request"`
			OK       bool  `json:"ok"`
		} `json:"admit"`
	}
	testvectors.Load(t, "quota", &v)
	const limit = int64(1) << 40 // test limit; the product limit is an open decision (Q-02)
	for _, c := range v.PartBytes {
		pb, err := PartBytes(c.Bytes, limit)
		n, _ := PartsTotal(c.Bytes, limit)
		if err != nil || pb != c.PartBytes || n != c.PartsTotal {
			t.Fatalf("PartBytes(%d) = %d,%d,%v; want %d,%d", c.Bytes, pb, n, err, c.PartBytes, c.PartsTotal)
		}
	}
	for _, c := range v.Derivative {
		if got, err := DerivativeEstimate(c.Bytes); err != nil || got != c.Estimate {
			t.Fatalf("DerivativeEstimate(%d) = %d,%v; want %d", c.Bytes, got, err, c.Estimate)
		}
	}
	for _, c := range v.Render {
		if got, err := RenderEstimate(c.Duration, c.Maxrate, c.Audio); err != nil || got != c.Estimate {
			t.Fatalf("RenderEstimate(%+v) = %d,%v", c, got, err)
		}
	}
	if len(v.Admit) != 6 {
		t.Fatalf("admit cases = %d", len(v.Admit))
	}
	for _, c := range v.Admit {
		if got := Admit(c.Used, c.Reserved, c.Limit, c.Request); got != c.OK {
			t.Fatalf("Admit(%+v) = %v", c, got)
		}
	}
}

func TestPartBytesErrorsAndRender3h(t *testing.T) {
	if _, err := PartBytes(0, 100); err != ErrBytesInvalid {
		t.Fatalf("0 bytes: %v", err)
	}
	if _, err := PartBytes(101, 100); err != ErrTooLarge {
		t.Fatalf("over limit: %v", err)
	}
	if got, err := RenderEstimate(7_620_480_000_000, 48_000_000, 192_000); err != nil || got != 68_379_268_864 {
		t.Fatalf("3h render estimate = %d, %v", got, err)
	}
	if _, err := RenderEstimate(1<<62, 1<<62, 0); err != ErrOverflow {
		t.Fatalf("overflow: %v", err)
	}
}
