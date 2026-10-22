package v2qualification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"math"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

// Parent quality/resource evidence is separate from every child operation report.
// Common.Backend is CPU only for the legacy common-field validator projection;
// no such projection is returned, published or inserted as a CPU qualification.
type MixedExpected struct {
	Common                     Expected
	Children                   []MixedChild
	Routes                     map[string]string
	MaxP95, MaxRSS, MaxScratch float64
}
type MixedReport struct {
	Schema          int               `json:"schema_version"`
	Scope           string            `json:"scope"`
	Operation       string            `json:"operation"`
	Version         string            `json:"version"`
	Model           string            `json:"model_lock_sha"`
	Runtime         string            `json:"runtime_lock_sha"`
	Device          string            `json:"device_profile_sha"`
	Dataset         string            `json:"dataset_sha"`
	Fixtures        []string          `json:"input_fixture_shas"`
	Checks          []Check           `json:"checks"`
	Metrics         MPSMetrics        `json:"metrics"`
	Status          string            `json:"status"`
	Issuer          uuid.UUID         `json:"issuer_id"`
	Created         json.RawMessage   `json:"created_at"`
	Children        []MixedChild      `json:"children"`
	Routes          map[string]string `json:"stage_routes"`
	DependenciesSHA string            `json:"dependencies_sha256"`
}

func VerifyMixed(raw []byte, pin string, expected MixedExpected) (MixedReport, error) {
	var out MixedReport
	if !digest(pin) || len(raw) < 2 || len(raw) > 8<<20 || expected.Common.Operation != "renderer.staged" || expected.Common.Version != "1" || expected.Common.Backend != "cpu" {
		return out, ErrUnqualified
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return out, ErrUnqualified
	}
	canon, e := canonical.Bytes(raw)
	if e != nil || !bytes.Equal(canon, raw) {
		return out, ErrUnqualified
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil {
		return out, ErrUnqualified
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return out, ErrUnqualified
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 17 {
		return out, ErrUnqualified
	}
	required := map[string]bool{}
	for _, id := range expected.Common.RequiredChecks {
		required[id] = true
	}
	for _, id := range []string{"rgb_golden", "alpha_golden", "generated_golden", "finite", "protected_unchanged", "outside_roi_unchanged", "stage_dependency_binding"} {
		if !required[id] {
			return out, ErrUnqualified
		}
	}
	if len(out.Fixtures) != len(expected.Common.TestFixtureSHAs) {
		return out, ErrUnqualified
	}
	for _, v := range []float64{expected.MaxP95, expected.MaxRSS, expected.MaxScratch} {
		if !bounded(v) || v <= 0 {
			return out, ErrUnqualified
		}
	}
	if out.Metrics.P50MS <= 0 || out.Metrics.RSSBytes < 1 || out.Metrics.ScratchBytes < 1 || math.Trunc(out.Metrics.RSSBytes) != out.Metrics.RSSBytes || math.Trunc(out.Metrics.ScratchBytes) != out.Metrics.ScratchBytes {
		return out, ErrUnqualified
	}
	if out.Schema != 2 || out.Scope != "renderer.staged.mixed" || out.Operation != "renderer.staged" || out.Version != "1" || out.Status != "passed" || !digest(out.DependenciesSHA) {
		return out, ErrUnqualified
	}
	want, e := canonical.Marshal(expected.Children)
	if e != nil {
		return out, e
	}
	actual, e := canonical.Marshal(out.Children)
	if e != nil || !bytes.Equal(want, actual) {
		return out, ErrUnqualified
	}
	sum = sha256.Sum256(actual)
	if hex.EncodeToString(sum[:]) != out.DependenciesSHA {
		return out, ErrUnqualified
	}
	want, e = canonical.Marshal(expected.Routes)
	if e != nil {
		return out, e
	}
	actual, e = canonical.Marshal(out.Routes)
	if e != nil || !bytes.Equal(want, actual) {
		return out, ErrUnqualified
	}
	// Validate the same closed dependency structure used by actual task admission.
	a := MixedAuthority{DependenciesSHA: out.DependenciesSHA, Routes: out.Routes, Children: out.Children}
	if a.validateDependencies() != nil {
		return out, ErrUnqualified
	}
	if !exactObject(fields["metrics"], []string{"p50_ms", "p95_ms", "peak_rss_bytes", "scratch_bytes"}) || expected.MaxP95 <= 0 || expected.MaxRSS <= 0 || expected.MaxScratch <= 0 || out.Metrics.P95MS > expected.MaxP95 || out.Metrics.RSSBytes > expected.MaxRSS || out.Metrics.ScratchBytes > expected.MaxScratch {
		return out, ErrUnqualified
	}
	for _, key := range []string{"scope", "children", "stage_routes", "dependencies_sha256"} {
		delete(fields, key)
	}
	fields["schema_version"] = json.RawMessage("1")
	fields["metrics"], e = canonical.Marshal(Metrics{P50MS: out.Metrics.P50MS, P95MS: out.Metrics.P95MS, PeakRSSBytes: out.Metrics.RSSBytes, ScratchBytes: out.Metrics.ScratchBytes})
	if e != nil {
		return out, e
	}
	projected, e := canonical.Marshal(fields)
	if e != nil {
		return out, e
	}
	sum = sha256.Sum256(projected)
	if _, e = Verify(projected, hex.EncodeToString(sum[:]), expected.Common); e != nil {
		return out, e
	}
	return out, nil
}
func (s Importer) ImportMixed(ctx context.Context, tenant, project, report, operator uuid.UUID, pin string, expected MixedExpected) (uuid.UUID, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || project == uuid.Nil || report == uuid.Nil || operator == uuid.Nil || !digest(pin) {
		return uuid.Nil, ErrUnqualified
	}
	var bucket string
	o := objectstore.Object{SHA256: pin, ContentType: "application/json"}
	e := s.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,bytes FROM photo.v2_qualification_report_object($1,$2,$3)`, tenant, report, pin).Scan(&bucket, &o.Key, &o.VersionID, &o.Bytes)
	if errors.Is(e, pgx.ErrNoRows) || bucket != s.Bucket || o.Bytes < 2 || o.Bytes > 8<<20 {
		return uuid.Nil, ErrUnqualified
	}
	if e != nil {
		return uuid.Nil, e
	}
	reader, e := s.Objects.Open(ctx, o)
	if e != nil {
		return uuid.Nil, e
	}
	defer reader.Close()
	raw, e := io.ReadAll(io.LimitReader(reader, 8<<20+1))
	if e != nil || int64(len(raw)) != o.Bytes {
		return uuid.Nil, ErrUnqualified
	}
	if _, e = VerifyMixed(raw, pin, expected); e != nil {
		return uuid.Nil, e
	}
	var result uuid.UUID
	e = s.DB.QueryRow(ctx, `SELECT photo.import_mixed_renderer_qualification($1,$2,$3,$4,$5,$6)`, tenant, project, report, pin, operator, string(raw)).Scan(&result)
	if e != nil {
		return uuid.Nil, e
	}
	if result == uuid.Nil {
		return uuid.Nil, ErrUnqualified
	}
	return result, nil
}
