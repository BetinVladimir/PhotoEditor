package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

type negativeEvidenceIO struct {
	report, cal   objectstore.Object
	r, c          []byte
	mode          string
	reads, closes int
	catalogs      int
}

func (s *negativeEvidenceIO) ResolveNegativeQualification(context.Context, auth.Identity, uuid.UUID, uuid.UUID, NegativeQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	s.catalogs++
	c := s.cal
	if s.mode == "changed" && s.reads > 0 {
		c.VersionID = "v2"
	}
	return "bucket", s.report, c, nil
}

type negativeEvidenceStream struct {
	io.Reader
	owner *negativeEvidenceIO
}

func (s negativeEvidenceStream) Close() error { s.owner.closes++; return nil }
func (s *negativeEvidenceIO) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	s.reads++
	raw := s.r
	if obj.Key == "cal" {
		raw = s.c
	}
	raw = append([]byte(nil), raw...)
	if s.mode == "corrupt" {
		raw[0] ^= 1
	}
	if s.mode == "oversize" {
		raw = append(raw, 32)
	}
	return negativeEvidenceStream{bytes.NewReader(raw), s}, nil
}
func TestNegativeQualificationVersionedObjectLoader(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Params      NegativeParams
		Calibration string
		Report      string
		Pin         NegativeQualificationPin
		Locks       v2qualification.DeploymentLocks
	}
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture")
	}
	for _, mode := range []string{"ok", "changed", "corrupt", "oversize"} {
		source := &negativeEvidenceIO{mode: mode, r: []byte(f.Report), c: []byte(f.Calibration), report: objectstore.Object{Key: "report", VersionID: "v1", SHA256: f.Pin.Report.SHA256, Bytes: int64(len(f.Report)), ContentType: "application/json"}, cal: objectstore.Object{Key: "cal", VersionID: "v1", SHA256: f.Pin.Calibration.SHA256, Bytes: int64(len(f.Calibration)), ContentType: "application/json"}}
		located, err := LoadNegativeQualificationEvidence(context.Background(), source, source, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), f.Pin, f.Locks, f.Params, f.Params.Base.CanvasHash)
		if mode == "ok" {
			if err != nil {
				t.Fatal(err)
			}
			for _, stale := range []bool{false, true} {
				tenant := uuid.New()
				tx := &negativeLocatedTx{negativeLockTx: negativeLockTx{tenant: tenant}, stale: stale}
				e := LockLocatedNegativeQualification(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, uuid.New(), located)
				if !tx.observed || !stale && e != nil || stale && e != v2input.ErrExpired {
					t.Fatal("located lock", e)
				}
			}
			got, e := located.Qualification()
			if e != nil {
				t.Fatal(e)
			}
			pin, e := got.Pin()
			if e != nil || pin != f.Pin {
				t.Fatal(e)
			}
		} else {
			if err == nil || source.reads != 1 {
				t.Fatal(mode, err, source.reads)
			}
		}
		if source.reads != source.closes {
			t.Fatal("unclosed stream")
		}
	}
}

type negativeLocatedTx struct {
	negativeLockTx
	stale    bool
	observed bool
}

func (t *negativeLocatedTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q != "SELECT photo.lock_v29_negative_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb)" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		var objects []NegativeQualificationObject
		if len(args) != 6 || json.Unmarshal([]byte(args[5].(string)), &objects) != nil || len(objects) != 2 {
			return v2input.ErrInvalid
		}
		t.observed = true
		*dest[0].(*bool) = !t.stale
		return nil
	})
}

func TestNegativeEvidencePreflightBeforeExternalCalls(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Params NegativeParams
		Pin    NegativeQualificationPin
		Locks  v2qualification.DeploymentLocks
	}
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture")
	}
	for _, mode := range []string{"params", "binding", "icc", "runtime", "model", "device", "dataset", "issuer"} {
		p, pin, icc := f.Params, f.Pin, f.Params.Base.CanvasHash
		switch mode {
		case "params":
			p.Strength = 2
		case "binding":
			p.Calibration.ArtifactID = p.Base.Source.ArtifactID
		case "icc":
			icc = "invalid"
		case "runtime":
			pin.RuntimeLockSHA = pin.ModelLockSHA
		case "model":
			pin.ModelLockSHA = pin.RuntimeLockSHA
		case "device":
			pin.DeviceProfileSHA = pin.RuntimeLockSHA
		case "dataset":
			pin.DatasetSHA = pin.RuntimeLockSHA
		case "issuer":
			pin.IssuerID = uuid.New()
		}
		source := &negativeEvidenceIO{}
		if _, err := LoadNegativeQualificationEvidence(context.Background(), source, source, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), pin, f.Locks, p, icc); err == nil || source.catalogs != 0 || source.reads != 0 {
			t.Fatal(mode, err, source.catalogs, source.reads)
		}
	}
}
