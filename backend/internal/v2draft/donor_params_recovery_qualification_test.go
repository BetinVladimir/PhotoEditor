package v2draft

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

type recoveryQualificationCatalogFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, v2portrait.DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error)

func (f recoveryQualificationCatalogFunc) ResolveDonorQualification(ctx context.Context, w auth.Identity, t, p uuid.UUID, pin v2portrait.DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
	return f(ctx, w, t, p, pin)
}
func TestRecoveryQualificationRejectsChangedLocations(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pin := v2portrait.DonorQualificationPin{Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}
	cal := objectstore.Object{Key: "cal", VersionID: "c1", SHA256: sha, Bytes: 100, ContentType: "application/json"}
	report := objectstore.Object{Key: "report", VersionID: "r1", SHA256: sha, Bytes: 2 << 20, ContentType: "application/json"}
	expected := []v2portrait.DonorQualificationObject{
		{Contract: "Calibration", ArtifactID: pin.Calibration.ArtifactID, Bucket: "photos", Key: cal.Key, Version: cal.VersionID, SHA256: sha, Bytes: cal.Bytes},
		{Contract: "Report", ArtifactID: pin.Report.ArtifactID, Bucket: "photos", Key: report.Key, Version: report.VersionID, SHA256: sha, Bytes: report.Bytes},
	}
	for _, mode := range []string{"ok", "bucket", "key", "version", "bytes", "mime", "sha", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			catalog := recoveryQualificationCatalogFunc(func(_ context.Context, _ auth.Identity, _, _ uuid.UUID, p v2portrait.DonorQualificationPin) (string, objectstore.Object, objectstore.Object, error) {
				calls++
				if p != pin {
					t.Fatal("changed pin")
				}
				b, r, c := "photos", report, cal
				switch mode {
				case "bucket":
					b = "other"
				case "key":
					c.Key = "other"
				case "version":
					r.VersionID = "r2"
				case "bytes":
					r.Bytes++
				case "mime":
					c.ContentType = "text/plain"
				case "sha":
					r.SHA256 = strings.Repeat("b", 64)
				case "error":
					return "", r, c, errors.New("catalog")
				case "cancel":
					cancel()
				}
				return b, r, c, nil
			})
			wrapper := donorRecoveryQualificationCatalog{catalog: catalog, pin: pin, expected: expected}
			bucket, r, c, err := wrapper.ResolveDonorQualification(ctx, auth.Identity{}, uuid.New(), uuid.New(), pin)
			if (err == nil) != (mode == "ok") || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if err != nil && (bucket != "" || r.Key != "" || c.Key != "") {
				t.Fatal("partial IO locators escaped")
			}
			if mode == "ok" && (bucket != "photos" || r != report || c != cal) {
				t.Fatal("reordered originals")
			}
			changed := pin
			changed.Report.ArtifactID = uuid.New()
			calls = 0
			if _, _, _, err := wrapper.ResolveDonorQualification(context.Background(), auth.Identity{}, uuid.New(), uuid.New(), changed); err == nil || calls != 0 {
				t.Fatal("changed recovery pin reached catalog")
			}
		})
	}
}
