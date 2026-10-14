package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
)

func analysisQualificationFixture(t *testing.T) (RelightAnalysisPlanRequest, []byte, v2qualification.Expected) {
	t.Helper()
	request := analysisPlanFixture(t)
	digest := func(c string) string { return strings.Repeat(c, 64) }
	report := v2qualification.Report{SchemaVersion: 1, Operation: request.Operation, Version: "1", ModelLockSHA: request.Runtime.ModelLockSHA, RuntimeLockSHA: request.Runtime.RuntimeLockSHA, DeviceProfileSHA: request.Runtime.DeviceProfileSHA, DatasetSHA: digest("d"), InputFixtureSHAs: []string{digest("e")}, Checks: []v2qualification.Check{{ID: "quality", Passed: true, EvidenceSHA: digest("f")}}, Metrics: v2qualification.Metrics{P50MS: 1, P95MS: 2, PeakRSSBytes: 1024, ScratchBytes: 1024}, Status: "passed", IssuerID: uuid.New(), CreatedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}
	raw, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	request.Runtime.Qualification.SHA256 = hex.EncodeToString(sum[:])
	expected := v2qualification.Expected{Operation: report.Operation, Version: "1", Backend: request.Runtime.Backend, ModelLockSHA: report.ModelLockSHA, RuntimeLockSHA: report.RuntimeLockSHA, DeviceProfileSHA: report.DeviceProfileSHA, DatasetSHA: report.DatasetSHA, IssuerID: report.IssuerID, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{digest("e"): {}}, EvidenceSHAs: map[string]struct{}{digest("f"): {}}, LicenseCleared: true}
	return request, raw, expected
}
func TestQualifiedAnalysisPlanBindsReportAndDeployment(t *testing.T) {
	request, raw, expected := analysisQualificationFixture(t)
	if _, err := CompileQualifiedRelightAnalysisPlan(context.Background(), request, raw, expected); err != nil {
		t.Fatal(err)
	}
	for _, fault := range []string{"backend", "operation", "version", "model", "runtime", "device", "issuer", "license", "hash", "bytes", "rss", "scratch", "vram"} {
		r := request
		e := expected
		body := append([]byte(nil), raw...)
		switch fault {
		case "backend":
			e.Backend = "mps"
		case "operation":
			e.Operation = "other"
		case "version":
			e.Version = "2"
		case "model":
			e.ModelLockSHA = strings.Repeat("0", 64)
		case "runtime":
			e.RuntimeLockSHA = strings.Repeat("0", 64)
		case "device":
			e.DeviceProfileSHA = strings.Repeat("0", 64)
		case "issuer":
			e.IssuerID = uuid.New()
		case "license":
			e.LicenseCleared = false
		case "hash":
			r.Runtime.Qualification.SHA256 = strings.Repeat("0", 64)
		case "bytes":
			body = append(body, ' ')
		case "rss":
			r.Runtime.Resources.RAMBytes = 1
		case "scratch":
			r.Runtime.Resources.ScratchBytes = 1
		case "vram":
			r.Runtime.Resources.VRAMBytes = -1
		}
		if _, err := CompileQualifiedRelightAnalysisPlan(context.Background(), r, body, e); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileQualifiedRelightAnalysisPlan(ctx, request, raw, expected); err != context.Canceled {
		t.Fatal(err)
	}
}

type analysisEvidenceCatalog struct {
	object   objectstore.Object
	calls    int
	changeAt int
}

func (c *analysisEvidenceCatalog) ResolveRelightAnalysisQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, RelightAnalysisRuntime) (string, objectstore.Object, error) {
	c.calls++
	o := c.object
	if c.changeAt > 0 && c.calls >= c.changeAt {
		o.VersionID = "replaced"
	}
	return "photos", o, nil
}

type analysisEvidenceObjects struct {
	raw    []byte
	opens  int
	closed bool
}
type analysisEvidenceReader struct {
	io.Reader
	owner *analysisEvidenceObjects
}

func (r *analysisEvidenceReader) Close() error { r.owner.closed = true; return nil }
func (o *analysisEvidenceObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return &analysisEvidenceReader{Reader: bytes.NewReader(o.raw), owner: o}, nil
}
func TestAnalysisEvidenceLoaderFencesVersionChangesAndBoundsRead(t *testing.T) {
	for _, fault := range []string{"none", "before_read", "after_read", "final", "oversize", "truncated", "unversioned"} {
		request, raw, expected := analysisQualificationFixture(t)
		catalog := &analysisEvidenceCatalog{object: objectstore.Object{Key: "qualified/report", VersionID: "version-1", ContentType: "application/json", SHA256: request.Runtime.Qualification.SHA256, Bytes: int64(len(raw))}}
		objects := &analysisEvidenceObjects{raw: raw}
		switch fault {
		case "before_read":
			catalog.changeAt = 2
		case "after_read":
			catalog.changeAt = 3
		case "final":
			catalog.changeAt = 4
		case "oversize":
			objects.raw = append(append([]byte(nil), raw...), 'x')
		case "truncated":
			objects.raw = raw[:len(raw)-1]
		case "unversioned":
			catalog.object.VersionID = "null"
		}
		_, err := LoadQualifiedRelightAnalysisPlan(context.Background(), catalog, objects, request, expected)
		if fault == "none" && err != nil {
			t.Fatal(err)
		}
		if fault != "none" && err == nil {
			t.Fatal("accepted", fault)
		}
		if (fault == "before_read" || fault == "unversioned") && objects.opens != 0 {
			t.Fatal("read before authority", fault)
		}
		if objects.opens > 0 && !objects.closed {
			t.Fatal("reader leaked", fault)
		}
	}
}
