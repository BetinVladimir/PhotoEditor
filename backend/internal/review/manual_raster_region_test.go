package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
)

func TestManualRasterRegionPythonSampleParity(t *testing.T) {
	raw, err := os.ReadFile("testdata/manual-raster-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Vectors   manualVectors `json:"vectors"`
		Target    []uint32      `json:"target_bits"`
		Protected []uint32      `json:"protected_bits"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	boundaryRaw, err := os.ReadFile("testdata/manual-raster-boundaries-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var boundaryCases []struct {
		Vectors   manualVectors `json:"vectors"`
		Target    []uint32      `json:"target_bits"`
		Protected []uint32      `json:"protected_bits"`
	}
	if err = json.Unmarshal(boundaryRaw, &boundaryCases); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, boundaryCases...)
	for ordinal, c := range cases {
		if !validManualVectors(c.Vectors, DefectSource{Canvas: c.Vectors.Canvas}) {
			t.Fatal("invalid original Python fixture")
		}
		body, err := canonical.Marshal(c.Vectors)
		if err != nil {
			t.Fatal(err)
		}
		sort.Slice(c.Vectors.Shapes, func(i, j int) bool { return c.Vectors.Shapes[i].Sequence < c.Vectors.Shapes[j].Sequence })
		plan := ManualRasterPlan{body: body, canvas: c.Vectors.Canvas, shapes: len(c.Vectors.Shapes), vectors: c.Vectors}
		pin := func(bits []uint32) masknpy.PinnedMask {
			header := []byte(fmt.Sprintf("{'descr':'<f4','fortran_order':False,'shape':(%d,%d)}\n", plan.canvas.Height, plan.canvas.Width))
			body := []byte{0x93, 'N', 'U', 'M', 'P', 'Y', 1, 0, 0, 0}
			binary.LittleEndian.PutUint16(body[8:], uint16(len(header)))
			body = append(body, header...)
			for _, value := range bits {
				var b [4]byte
				binary.LittleEndian.PutUint32(b[:], value)
				body = append(body, b[:]...)
			}
			sum := sha256.Sum256(body)
			return masknpy.PinnedMask{Reader: bytes.NewReader(body), Bytes: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
		}
		_, err = masknpy.VerifyRasterProbabilityNPY(context.Background(), pin(c.Target), pin(c.Protected), plan.canvas.Width, plan.canvas.Height, func(ctx context.Context, x, y, n int64) ([]float32, []float32, error) {
			return plan.RasterizeRegion(ctx, x, y, n, 1)
		})
		if err != nil {
			t.Fatal("original Python NPY raster mismatch", ordinal, err)
		}
		for _, size := range []int64{7, 32, 1024} {
			for y := int64(0); y < plan.canvas.Height; y += size {
				for x := int64(0); x < plan.canvas.Width; x += size {
					width, height := min(size, plan.canvas.Width-x), min(size, plan.canvas.Height-y)
					target, protected, err := plan.RasterizeRegion(context.Background(), x, y, width, height)
					if err != nil {
						t.Fatal(err)
					}
					for row := int64(0); row < height; row++ {
						for col := int64(0); col < width; col++ {
							i := row*width + col
							j := (y+row)*plan.canvas.Width + x + col
							if math.Float32bits(target[i]) != c.Target[j] || math.Float32bits(protected[i]) != c.Protected[j] {
								t.Fatalf("case%d at(%d,%d): target%08x/%08x protected%08x/%08x", ordinal, x+col, y+row, math.Float32bits(target[i]), c.Target[j], math.Float32bits(protected[i]), c.Protected[j])
							}
						}
					}
				}
			}
		}
		for _, region := range [][4]int64{{-1, 0, 1, 1}, {0, 0, 0, 1}, {0, 0, 1025, 1}, {plan.canvas.Width, 0, 1, 1}} {
			a, b, err := plan.RasterizeRegion(context.Background(), region[0], region[1], region[2], region[3])
			if err == nil || a != nil || b != nil {
				t.Fatal("invalid region accepted")
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		a, b, err := plan.RasterizeRegion(ctx, 0, 0, 1, 1)
		if !errors.Is(err, context.Canceled) || a != nil || b != nil {
			t.Fatal("cancelled raster accepted")
		}
	}
	if a, b, err := (ManualRasterPlan{}).RasterizeRegion(context.Background(), 0, 0, 1, 1); err == nil || a != nil || b != nil {
		t.Fatal("zero plan accepted")
	}
}
