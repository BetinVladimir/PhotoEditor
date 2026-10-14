package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type donorQualificationTestCatalog struct {
	report, calibration objectstore.Object
	denied              bool
	calls               int
}

func (s *donorQualificationTestCatalog) ResolveDonorQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	s.calls++
	if s.denied {
		return "", objectstore.Object{}, objectstore.Object{}, errors.New("denied")
	}
	return "test", s.report, s.calibration, nil
}

type donorQualificationTestReader struct {
	report, calibration []byte
	calls               int
	corrupt             bool
	onOpen              func()
}

func (s *donorQualificationTestReader) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.calls++
	if s.onOpen != nil {
		s.onOpen()
	}
	raw := s.report
	if obj.Key == "calibration" {
		raw = s.calibration
	}
	raw = append([]byte(nil), raw...)
	if s.corrupt {
		raw[0] ^= 1
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}
func checkLocatedDonorQualification(t *testing.T, verified VerifiedDonorQualification) {
	t.Helper()
	report := objectstore.Object{Key: "report", VersionID: "r1", SHA256: verified.pin.Report.SHA256, Bytes: int64(len(verified.reportRaw)), ContentType: "application/json"}
	calibration := objectstore.Object{Key: "calibration", VersionID: "c1", SHA256: verified.pin.Calibration.SHA256, Bytes: int64(len(verified.calibrationRaw)), ContentType: "application/json"}
	checkSQLDonorQualificationCatalog(t, verified, report, calibration)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	for _, mode := range []string{"ok", "denied", "version", "size", "corrupt", "cancelled", "revoked_during_read", "version_during_read", "bad_calibration"} {
		catalog := &donorQualificationTestCatalog{report: report, calibration: calibration}
		reader := &donorQualificationTestReader{report: verified.reportRaw, calibration: verified.calibrationRaw}
		ctx := context.Background()
		switch mode {
		case "revoked_during_read":
			reader.onOpen = func() { catalog.denied = true }
		case "version_during_read":
			reader.onOpen = func() { catalog.calibration.VersionID = "changed" }
		case "bad_calibration":
			catalog.calibration.Bytes = 2 << 20
		case "denied":
			catalog.denied = true
		case "version":
			catalog.report.VersionID = "null"
		case "size":
			catalog.report.Bytes = 9 << 20
		case "corrupt":
			reader.corrupt = true
		case "cancelled":
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		got, err := LoadDonorQualificationEvidence(ctx, catalog, reader, who, tenant, project, verified.pin, verified.locks)
		if mode != "ok" {
			if err == nil || got.verified.reportRaw != nil {
				t.Fatal("partial located evidence", mode)
			}
			if mode != "corrupt" && mode != "revoked_during_read" && mode != "version_during_read" && reader.calls != 0 {
				t.Fatal("bad scope performed object reads", mode)
			}
			if (mode == "revoked_during_read" || mode == "version_during_read") && reader.calls != 1 {
				t.Fatal("read continued after authority change")
			}
			continue
		}
		if err != nil || reader.calls != 2 {
			t.Fatal("located evidence", err)
		}
		metadata, err := got.Objects()
		if err != nil || len(metadata) != 2 {
			t.Fatal("recovery locations", err)
		}
		metadata[0].Version = "changed"
		owned, err := got.Objects()
		if err != nil || owned[0].Version != calibration.VersionID {
			t.Fatal("mutable recovery locations", err)
		}
		for _, ready := range []bool{true, false} {
			tx := &donorIdentityTx{role: "photo_scheduler", ready: ready}
			err := LockLocatedDonorQualification(ctx, tx, who, tenant, project, got)
			if (err == nil) != ready || tx.calls != 2 {
				t.Fatal("located lock", ready, err)
			}
			var retained [2]DonorQualificationObject
			if json.Unmarshal([]byte(tx.args[5].(string)), &retained) != nil || retained != got.objects {
				t.Fatal("location evidence changed")
			}
		}
	}
	for _, mode := range []string{"bucket", "sha", "bytes", "media", "key", "version"} {
		r, bucket := report, "test"
		switch mode {
		case "bucket":
			bucket = ""
		case "sha":
			r.SHA256 = calibration.SHA256
		case "bytes":
			r.Bytes++
		case "media":
			r.ContentType = "text/plain"
		case "key":
			r.Key = ""
		case "version":
			r.VersionID = "null"
		}
		if got, err := BindDonorQualificationObjects(verified, bucket, r, calibration); err == nil || got.verified.reportRaw != nil {
			t.Fatal("bound invalid object", mode)
		}
	}
}

func checkSQLDonorQualificationCatalog(t *testing.T, verified VerifiedDonorQualification, report, calibration objectstore.Object) {
	t.Helper()
	located, err := BindDonorQualificationObjects(verified, "test", report, calibration)
	if err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	for _, mode := range []string{"ok", "role", "null", "bucket", "version", "order", "sha", "unknown", "commit", "cancelled"} {
		values := located.objects
		switch mode {
		case "bucket":
			values[0].Bucket = "other"
		case "version":
			values[0].Version = "null"
		case "order":
			values[0], values[1] = values[1], values[0]
		case "sha":
			values[0].SHA256 = verified.pin.Report.SHA256
		}
		raw, err := canonical.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		if mode == "null" {
			raw = []byte("null")
		}
		if mode == "unknown" {
			var fields []map[string]any
			if json.Unmarshal(raw, &fields) != nil {
				t.Fatal("fixture")
			}
			fields[0]["extra"] = true
			raw, err = canonical.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
		}
		tx := &donorIssueTransaction{donorIdentityTx: donorIdentityTx{role: "photo_scheduler", raw: raw}, failCommit: mode == "commit"}
		if mode == "role" {
			tx.role = "photo_api"
		}
		db := &donorIssueDB{tx: tx}
		catalog := SQLDonorQualificationCatalog{DB: db, Bucket: "test"}
		ctx := context.Background()
		if mode == "cancelled" {
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		bucket, r, c, err := catalog.ResolveDonorQualification(ctx, who, tenant, project, verified.pin)
		if mode == "ok" {
			if err != nil || bucket != "test" || r != report || c != calibration || tx.commits != 1 || tx.tenantArg != tenant.String() {
				t.Fatal("SQL source catalog", err)
			}
		} else if err == nil || bucket != "" || r != (objectstore.Object{}) || c != (objectstore.Object{}) {
			t.Fatal("partial SQL sources", mode, err)
		}
		if mode != "ok" && mode != "commit" && tx.commits != 0 {
			t.Fatal("invalid sources committed", mode)
		}
		if mode == "cancelled" && db.begins != 0 {
			t.Fatal("cancelled catalog began transaction")
		}
	}
}
