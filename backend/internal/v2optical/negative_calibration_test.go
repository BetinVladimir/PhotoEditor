package v2optical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"testing"
)

func TestNegativeCalibrationOriginalBytes(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-calibration.json")
	if err != nil {
		t.Fatal(err)
	}
	paramsRaw, err := os.ReadFile("testdata/negative-params.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseNegativeParams(paramsRaw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	p.Calibration.SHA256 = hex.EncodeToString(sum[:])
	c, err := VerifyNegativeCalibration(p, raw, p.Base.CanvasHash)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{append(append([]byte{}, raw...), 32), bytes.Replace(raw, []byte(`"density_range":[1,1,1]`), []byte(`"density_range":[0,1,1]`), 1), bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"extra":0`), 1)} {
		q := p
		sum := sha256.Sum256(body)
		q.Calibration.SHA256 = hex.EncodeToString(sum[:])
		if _, err := VerifyNegativeCalibration(q, body, c.InputICCSHA); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	if _, err := VerifyNegativeCalibration(p, raw, p.Calibration.SHA256); err == nil {
		t.Fatal("accepted wrong ICC")
	}
	c.DensityMatrix[0][0] = math.NaN()
	if _, err := c.Compile(); err == nil {
		t.Fatal("accepted NaN")
	}
}
