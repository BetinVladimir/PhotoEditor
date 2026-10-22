package v2qualification

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

var ErrUnqualified = errors.New("QUALIFICATION_UNAVAILABLE")

type Check struct {
	ID          string `json:"id"`
	Passed      bool   `json:"passed"`
	EvidenceSHA string `json:"evidence_sha"`
}

type Metrics struct {
	P50MS         float64 `json:"p50_ms"`
	P95MS         float64 `json:"p95_ms"`
	PeakVRAMBytes float64 `json:"peak_vram_bytes"`
	PeakRSSBytes  float64 `json:"peak_rss_bytes"`
	ScratchBytes  float64 `json:"scratch_bytes"`
}

type Report struct {
	SchemaVersion    int       `json:"schema_version"`
	Operation        string    `json:"operation"`
	Version          string    `json:"version"`
	ModelLockSHA     string    `json:"model_lock_sha"`
	RuntimeLockSHA   string    `json:"runtime_lock_sha"`
	DeviceProfileSHA string    `json:"device_profile_sha"`
	DatasetSHA       string    `json:"dataset_sha"`
	InputFixtureSHAs []string  `json:"input_fixture_shas"`
	Checks           []Check   `json:"checks"`
	Metrics          Metrics   `json:"metrics"`
	Status           string    `json:"status"`
	IssuerID         uuid.UUID `json:"issuer_id"`
	CreatedAt        time.Time `json:"created_at"`
}

// Expected is loaded from deployment-controlled locks and acceptance policy,
// never from the report or a public API request. Backend is stored in SQL only
// after this binding has succeeded.
type Expected struct {
	MPSResources *MPSResourcePolicy `json:",omitempty"`

	Operation, Version, Backend                                string
	ModelLockSHA, RuntimeLockSHA, DeviceProfileSHA, DatasetSHA string
	IssuerID                                                   uuid.UUID
	RequiredChecks                                             []string
	CalibrationSHAs                                            map[string]struct{}
	TestFixtureSHAs                                            map[string]struct{}
	EvidenceSHAs                                               map[string]struct{}
	LicenseCleared                                             bool
}

func digest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func bounded(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) &&
		value >= 0 && value <= 9007199254740991
}

func exactObject(raw []byte, names []string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != len(names) {
		return false
	}
	for _, name := range names {
		if _, present := fields[name]; !present {
			return false
		}
	}
	if _, top := fields["metrics"]; top {
		if !exactObject(fields["metrics"], []string{"p50_ms", "p95_ms",
			"peak_vram_bytes", "peak_rss_bytes", "scratch_bytes"}) {
			return false
		}
	}
	if rawChecks, top := fields["checks"]; top {
		var checks []json.RawMessage
		if json.Unmarshal(rawChecks, &checks) != nil {
			return false
		}
		for _, check := range checks {
			if !exactObject(check, []string{"id", "passed", "evidence_sha"}) {
				return false
			}
		}
	}
	return true
}

// Verify checks exact JCS bytes, the artifact SHA and deployment-pinned inputs.
// The caller must independently authenticate the issuer and build the trusted
// fixture/evidence sets from verified objects before insertion.
func Verify(raw []byte, expectedSHA string, expected Expected) (Report, error) {
	var report Report
	if expected.MPSResources != nil || len(raw) < 2 || len(raw) > 8*1024*1024 || !digest(expectedSHA) ||
		!expected.LicenseCleared || expected.IssuerID == uuid.Nil ||
		len(expected.RequiredChecks) == 0 || len(expected.TestFixtureSHAs) == 0 ||
		len(expected.EvidenceSHAs) == 0 ||
		(expected.Backend != "cpu" && expected.Backend != "cuda" && expected.Backend != "mps") {
		return report, ErrUnqualified
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, canonicalBody) {
		return report, ErrUnqualified
	}
	if !exactObject(raw, []string{"schema_version", "operation", "version",
		"model_lock_sha", "runtime_lock_sha", "device_profile_sha",
		"dataset_sha", "input_fixture_shas", "checks", "metrics",
		"status", "issuer_id", "created_at"}) {
		return report, ErrUnqualified
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expectedSHA {
		return report, ErrUnqualified
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&report) != nil || report.SchemaVersion != 1 ||
		report.Operation != expected.Operation || report.Version != expected.Version ||
		report.ModelLockSHA != expected.ModelLockSHA ||
		report.RuntimeLockSHA != expected.RuntimeLockSHA ||
		report.DeviceProfileSHA != expected.DeviceProfileSHA ||
		report.DatasetSHA != expected.DatasetSHA || report.IssuerID != expected.IssuerID ||
		report.Status != "passed" || report.CreatedAt.IsZero() ||
		len(report.Operation) == 0 || len(report.Operation) > 80 ||
		len(report.Version) == 0 || len(report.Version) > 20 ||
		!digest(report.ModelLockSHA) || !digest(report.RuntimeLockSHA) ||
		!digest(report.DeviceProfileSHA) || !digest(report.DatasetSHA) ||
		len(report.InputFixtureSHAs) < 1 || len(report.InputFixtureSHAs) > 10000 ||
		len(report.Checks) < 1 || len(report.Checks) > 1000 ||
		!bounded(report.Metrics.P50MS) || !bounded(report.Metrics.P95MS) ||
		!bounded(report.Metrics.PeakVRAMBytes) ||
		!bounded(report.Metrics.PeakRSSBytes) || !bounded(report.Metrics.ScratchBytes) ||
		report.Metrics.P95MS < report.Metrics.P50MS {
		return Report{}, ErrUnqualified
	}
	fixtures := make(map[string]struct{}, len(report.InputFixtureSHAs))
	for _, value := range report.InputFixtureSHAs {
		if !digest(value) {
			return Report{}, ErrUnqualified
		}
		if _, duplicate := fixtures[value]; duplicate {
			return Report{}, ErrUnqualified
		}
		if _, overlap := expected.CalibrationSHAs[value]; overlap {
			return Report{}, ErrUnqualified
		}
		if _, trusted := expected.TestFixtureSHAs[value]; !trusted {
			return Report{}, ErrUnqualified
		}
		fixtures[value] = struct{}{}
	}
	checks := make(map[string]struct{}, len(report.Checks))
	for _, check := range report.Checks {
		if len(check.ID) == 0 || len(check.ID) > 100 || !check.Passed ||
			!digest(check.EvidenceSHA) {
			return Report{}, ErrUnqualified
		}
		if _, trusted := expected.EvidenceSHAs[check.EvidenceSHA]; !trusted {
			return Report{}, ErrUnqualified
		}
		if _, duplicate := checks[check.ID]; duplicate {
			return Report{}, ErrUnqualified
		}
		checks[check.ID] = struct{}{}
	}
	for _, required := range expected.RequiredChecks {
		if _, exists := checks[required]; !exists {
			return Report{}, ErrUnqualified
		}
	}
	return report, nil
}
