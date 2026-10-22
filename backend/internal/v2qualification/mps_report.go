package v2qualification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"photoeditor/backend/internal/canonical"
)

const MPSAllocatorMetric = "pytorch_allocator_conservative_ceiling"

// MPSResourcePolicy is deployment controlled, never selected by report bytes.
// Allocator ceiling is a conservative bound, not an exact VRAM peak. External
// Metal allocators are outside this receipt and must not claim this metric.
type MPSResourcePolicy struct {
	Metric         string  `json:"metric"`
	AllocatorBytes float64 `json:"allocator_ceiling_bytes"`
	P95MS          float64 `json:"p95_ms"`
	RSSBytes       float64 `json:"peak_rss_bytes"`
	ScratchBytes   float64 `json:"scratch_bytes"`
}
type MPSMemoryEvidence struct {
	Metric        string  `json:"metric"`
	Ceiling       float64 `json:"allocator_ceiling_bytes"`
	SampleMax     float64 `json:"driver_sample_max_bytes"`
	PeakAvailable bool    `json:"peak_measurement_available"`
}
type MPSMetrics struct {
	P50MS        float64 `json:"p50_ms"`
	P95MS        float64 `json:"p95_ms"`
	RSSBytes     float64 `json:"peak_rss_bytes"`
	ScratchBytes float64 `json:"scratch_bytes"`
}
type MPSReport struct {
	// Common is validated from a private semantic projection only. Its zero GPU
	// value is never returned as a measured peak or persisted as a report.
	common  Report
	GPU     MPSMemoryEvidence
	Metrics MPSMetrics
}

func validMPSResourcePolicy(p MPSResourcePolicy) bool {
	return p.Metric == MPSAllocatorMetric && bounded(p.AllocatorBytes) && p.AllocatorBytes >= 1 && math.Trunc(p.AllocatorBytes) == p.AllocatorBytes && bounded(p.P95MS) && p.P95MS > 0 && bounded(p.RSSBytes) && p.RSSBytes > 0 && math.Trunc(p.RSSBytes) == p.RSSBytes && bounded(p.ScratchBytes) && p.ScratchBytes > 0 && math.Trunc(p.ScratchBytes) == p.ScratchBytes
}

// MPSOperation is actual model-stage compute, never host composition.
func MPSOperation(operation string) bool {
	switch operation {
	case "depth.predict", "face.detect", "face.landmarks", "face.parse", "features.scene", "scene.parse", "mask.person", "mask.prompt", "pose.detect", "session.describe", "image.denoise", "image.upscale", "image.inpaint", "image.outpaint", "image.generate":
		return true
	}
	return false
}
func VerifyMPS(raw []byte, expectedSHA string, expected Expected) (MPSReport, error) {
	var out MPSReport
	if expected.MPSResources == nil || !validMPSResourcePolicy(*expected.MPSResources) || expected.Backend != "mps" || !MPSOperation(expected.Operation) || expected.Version != "1" || !digest(expectedSHA) || len(raw) < 2 || len(raw) > 8<<20 {
		return out, ErrUnqualified
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expectedSHA {
		return out, ErrUnqualified
	}
	body, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(body, raw) {
		return out, ErrUnqualified
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 15 {
		return out, ErrUnqualified
	}
	for _, key := range []string{"schema_version", "operation", "version", "model_lock_sha", "runtime_lock_sha", "device_profile_sha", "dataset_sha", "input_fixture_shas", "checks", "metrics", "status", "issuer_id", "created_at", "backend", "gpu_memory"} {
		if _, ok := fields[key]; !ok {
			return out, ErrUnqualified
		}
	}
	if string(fields["schema_version"]) != "2" || string(fields["backend"]) != `"mps"` || string(fields["status"]) != `"passed"` {
		return out, ErrUnqualified
	}
	if !exactObject(fields["gpu_memory"], []string{"metric", "allocator_ceiling_bytes", "driver_sample_max_bytes", "peak_measurement_available"}) || !exactObject(fields["metrics"], []string{"p50_ms", "p95_ms", "peak_rss_bytes", "scratch_bytes"}) {
		return out, ErrUnqualified
	}
	var gpuFields map[string]json.RawMessage
	_ = json.Unmarshal(fields["gpu_memory"], &gpuFields)
	if string(gpuFields["peak_measurement_available"]) != "false" || json.Unmarshal(fields["gpu_memory"], &out.GPU) != nil || json.Unmarshal(fields["metrics"], &out.Metrics) != nil {
		return MPSReport{}, ErrUnqualified
	}
	p := expected.MPSResources
	m := out.Metrics
	g := out.GPU
	if g.Metric != p.Metric || g.PeakAvailable || !bounded(g.Ceiling) || g.Ceiling < 1 || math.Trunc(g.Ceiling) != g.Ceiling || g.Ceiling > p.AllocatorBytes || !bounded(g.SampleMax) || g.SampleMax > g.Ceiling || math.Trunc(g.SampleMax) != g.SampleMax || !bounded(m.P50MS) || !bounded(m.P95MS) || m.P50MS <= 0 || m.P95MS < m.P50MS || m.P95MS > p.P95MS || !bounded(m.RSSBytes) || m.RSSBytes < 1 || math.Trunc(m.RSSBytes) != m.RSSBytes || m.RSSBytes > p.RSSBytes || !bounded(m.ScratchBytes) || m.ScratchBytes > p.ScratchBytes || math.Trunc(m.ScratchBytes) != m.ScratchBytes {
		return MPSReport{}, ErrUnqualified
	}
	required := map[string]bool{}
	for _, id := range expected.RequiredChecks {
		required[id] = true
	}
	if !required["metal_real_inference"] || !required["gpu_memory_envelope"] {
		return MPSReport{}, ErrUnqualified
	}
	var fixtureSHAs []string
	if json.Unmarshal(fields["input_fixture_shas"], &fixtureSHAs) != nil || len(fixtureSHAs) != len(expected.TestFixtureSHAs) {
		return out, ErrUnqualified
	}
	delete(fields, "backend")
	delete(fields, "gpu_memory")
	fields["schema_version"] = json.RawMessage("1")
	projectedMetrics, _ := canonical.Marshal(Metrics{P50MS: m.P50MS, P95MS: m.P95MS, PeakRSSBytes: m.RSSBytes, ScratchBytes: m.ScratchBytes})
	fields["metrics"] = projectedMetrics
	projected, err := canonical.Marshal(fields)
	if err != nil {
		return MPSReport{}, ErrUnqualified
	}
	projectedSHA := sha256.Sum256(projected)
	commonExpected := expected
	commonExpected.MPSResources = nil
	common, err := Verify(projected, hex.EncodeToString(projectedSHA[:]), commonExpected)
	if err != nil {
		return MPSReport{}, err
	}
	out.common = common
	return out, nil
}

// VerifyImportReport dispatches only from protected deployment policy.
func VerifyImportReport(raw []byte, reportSHA string, expected Expected) error {
	if expected.MPSResources != nil {
		_, err := VerifyMPS(raw, reportSHA, expected)
		return err
	}
	_, err := Verify(raw, reportSHA, expected)
	return err
}
