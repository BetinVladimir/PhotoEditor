package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func segmentCalibrationEnvelope(t *testing.T, change func(map[string]any)) ([]byte, string, RenderQualification) {
	t.Helper()
	c := testRenderCalibration()
	c.Qualification.Backend = "cpu"
	for i := range c.Samples {
		c.Samples[i].Provider.VRAMBytes = 0
	}
	envelope := map[string]any{"schema_version": 1, "operation": "render.authored_segment", "operation_version": "1", "kernel_version": AuthoredSegmentKernelVersion, "contracts_sha": AuthoredSegmentSchemasSHA, "calibrations": map[string]any{"schema_version": 1, "profiles": []RenderCalibration{c}}}
	if change != nil {
		change(envelope)
	}
	raw, err := canonical.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	return raw, hex.EncodeToString(h[:]), c.Qualification
}
func TestAuthoredSegmentCalibrationIsolationAndCopy(t *testing.T) {
	raw, pin, q := segmentCalibrationEnvelope(t, nil)
	c, err := NewPinnedAuthoredSegmentCalibrationCatalog(raw, pin)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.ResolveAuthoredSegment(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	first.Samples[0].Width = 1
	next, err := c.ResolveAuthoredSegment(context.Background(), q)
	if err != nil || next.Samples[0].Width != 400 {
		t.Fatal("mutable calibration", err)
	}
	q.Backend = "mps"
	if _, err = c.ResolveAuthoredSegment(context.Background(), q); err == nil {
		t.Fatal("noncpu admitted")
	}
}
func TestAuthoredSegmentCalibrationRejectsWrongBuildEvenWithValidPin(t *testing.T) {
	for field, value := range map[string]any{"schema_version": 2, "operation": "render.compose", "operation_version": "2", "kernel_version": "v2.compose.2", "contracts_sha": "53d28", "unknown": true, "calibrations": nil} {
		t.Run(field, func(t *testing.T) {
			raw, pin, _ := segmentCalibrationEnvelope(t, func(e map[string]any) { e[field] = value })
			if _, err := NewPinnedAuthoredSegmentCalibrationCatalog(raw, pin); err == nil {
				t.Fatal("wrong build accepted")
			}
		})
	}
	raw, _, _ := segmentCalibrationEnvelope(t, nil)
	if _, err := NewPinnedAuthoredSegmentCalibrationCatalog(raw, ""); err == nil {
		t.Fatal("unpinned accepted")
	}
}
func TestAuthoredSegmentCalibrationCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var c *PinnedAuthoredSegmentCalibrationCatalog
	if _, err := c.ResolveAuthoredSegment(ctx, RenderQualification{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAuthoredSegmentCalibrationRefusesNonCPUInstalledProfiles(t *testing.T) {
	for _, backend := range []string{"cuda", "mps"} {
		t.Run(backend, func(t *testing.T) {
			raw, pin, _ := segmentCalibrationEnvelope(t, func(e map[string]any) {
				c := testRenderCalibration()
				c.Qualification.Backend = backend
				e["calibrations"] = map[string]any{"schema_version": 1, "profiles": []RenderCalibration{c}}
			})
			if _, err := NewPinnedAuthoredSegmentCalibrationCatalog(raw, pin); err == nil {
				t.Fatal("nonCPU measured profile accepted")
			}
		})
	}
}
