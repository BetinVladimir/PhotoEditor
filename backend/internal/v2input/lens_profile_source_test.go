package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type lensProfileObjectRow struct{ object objectstore.Object }

func (r lensProfileObjectRow) Scan(d ...any) error {
	*d[0].(*string) = "photos"
	*d[1].(*string) = r.object.Key
	*d[2].(*string) = r.object.VersionID
	*d[3].(*int64) = r.object.Bytes
	*d[4].(*string) = r.object.ContentType
	return nil
}

type lensProfileDB struct {
	objects []objectstore.Object
	calls   int
}

func (d *lensProfileDB) QueryRow(context.Context, string, ...any) pgx.Row {
	obj := d.objects[d.calls]
	d.calls++
	return lensProfileObjectRow{obj}
}

type lensProfileObjects map[string][]byte

func (o lensProfileObjects) Open(_ context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o[obj.Key])), nil
}
func TestLensProfileSourceVerifiesPinnedReportBytes(t *testing.T) {
	report := []byte(`{"operator_evidence":"synthetic IO fixture"}`)
	hash := sha256.Sum256(report)
	reportSHA := hex.EncodeToString(hash[:])
	profile := LensProfileDocument{SchemaVersion: 1, CameraModel: "Camera", LensModel: "Lens", FocalMM: 50, Aperture: 2, CX: .5, CY: .5, K: []float64{0, 0, 0}, P: []float64{0, 0}, Vignette: []float64{0, 0, 0}, CARedScale: 1, CABlueScale: 1, Qualification: Ref{uuid.New(), reportSHA}}
	raw, _ := canonical.Marshal(profile)
	profileSHA, _ := canonical.Hash(profile)
	var coeff map[string]json.RawMessage
	_ = json.Unmarshal(raw, &coeff)
	delete(coeff, "qualification")
	coefficientSHA, _ := canonical.Hash(coeff)
	pin := LensQualificationPin{profile.Qualification, coefficientSHA, reportSHA, reportSHA}
	descriptor := func(key string, size int64) objectstore.Object {
		return objectstore.Object{Key: key, VersionID: "v1", Bytes: size, ContentType: "application/json"}
	}
	run := func(body []byte, version string) ([]byte, error, int) {
		db := &lensProfileDB{objects: []objectstore.Object{descriptor("profile", int64(len(raw))), descriptor("report", int64(len(report)))}}
		db.objects[1].VersionID = version
		source := SQLLensProfileSource{DB: db, Objects: lensProfileObjects{"profile": raw, "report": body}, Bucket: "photos", RuntimeLockSHA: reportSHA, QualificationPins: []LensQualificationPin{pin}}
		result, err := source.Load(context.Background(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), Ref{uuid.New(), profileSHA})
		return result, err, db.calls
	}
	if actual, err, calls := run(report, "v1"); err != nil || calls != 2 || !bytes.Equal(actual, raw) {
		t.Fatal("verified profile/report", err, calls)
	}
	for _, body := range [][]byte{report[:len(report)-1], append(append([]byte(nil), report...), byte(' ')), bytes.Repeat([]byte{'x'}, len(report))} {
		if result, err, _ := run(body, "v1"); err == nil || result != nil {
			t.Fatal("report byte substitution admitted")
		}
	}
	if result, err, _ := run(report, "null"); err == nil || result != nil {
		t.Fatal("unpinned report admitted")
	}
}
