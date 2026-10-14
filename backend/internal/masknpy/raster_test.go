package masknpy

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestRasterProbabilityExactOriginalStreams(t *testing.T) {
	values := make([]float32, 2050)
	for i := range values {
		values[i] = .25
	}
	body := arrayFixture("(2,1025)", values)
	calls := 0
	expected := func(ctx context.Context, x, y, n int64) ([]float32, []float32, error) {
		calls++
		if x != 0 && x != 1024 || y < 0 || y > 1 {
			t.Fatal("invalid reconstruction coordinates")
		}
		a := make([]float32, n)
		b := make([]float32, n)
		for i := range a {
			a[i] = .25
			b[i] = .25
		}
		return a, b, nil
	}
	if n, err := VerifyRasterProbabilityNPY(context.Background(), pinnedMask(body), pinnedMask(body), 1025, 2, expected); err != nil || n != 2050 || calls != 4 {
		t.Fatal(n, err, calls)
	}
	changed := append([]float32{}, values...)
	changed[2049] = math.Nextafter32(.25, 1)
	for _, bad := range [][]byte{arrayFixture("(2,1025)", changed), body[:len(body)-1], append(append([]byte{}, body...), 0)} {
		if n, err := VerifyRasterProbabilityNPY(context.Background(), pinnedMask(body), pinnedMask(bad), 1025, 2, expected); err == nil || n != 0 {
			t.Fatal("changed original stream accepted")
		}
	}
	wrong := pinnedMask(body)
	wrong.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if n, err := VerifyRasterProbabilityNPY(context.Background(), wrong, pinnedMask(body), 1025, 2, expected); err == nil || n != 0 {
		t.Fatal("wrong original SHA accepted")
	}
	failure := errors.New("reconstruction failed")
	if n, err := VerifyRasterProbabilityNPY(context.Background(), pinnedMask(body), pinnedMask(body), 1025, 2, func(context.Context, int64, int64, int64) ([]float32, []float32, error) { return nil, nil, failure }); !errors.Is(err, failure) || n != 0 {
		t.Fatal("reconstruction error lost")
	}
	if n, err := VerifyRasterProbabilityNPY(context.Background(), pinnedMask(body), pinnedMask(body), 1025, 2, func(context.Context, int64, int64, int64) ([]float32, []float32, error) { return nil, nil, nil }); err == nil || n != 0 {
		t.Fatal("missing expected values accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n, err := VerifyRasterProbabilityNPY(ctx, pinnedMask(body), pinnedMask(body), 1025, 2, expected); !errors.Is(err, context.Canceled) || n != 0 {
		t.Fatal("cancelled streams accepted")
	}
}
