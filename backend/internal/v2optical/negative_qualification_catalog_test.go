package v2optical

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"testing"
	"time"
)

type negativeCatalogDB struct{ tx *negativeCatalogTx }

func (d negativeCatalogDB) Begin(ctx context.Context) (pgx.Tx, error) { return d.tx, nil }

type negativeCatalogTx struct {
	negativeLockTx
	locations []NegativeQualificationObject
	committed bool
	cleaned   bool
}

func (t *negativeCatalogTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if q != "SELECT photo.v29_negative_qualification_sources($1,$2,$3,$4,$5::jsonb)" {
			return errors.New("query")
		}
		raw, err := canonical.Marshal(t.locations)
		*dest[0].(*[]byte) = raw
		return err
	})
}
func (t *negativeCatalogTx) Commit(ctx context.Context) error {
	if t.mode == "commit" {
		return errors.New("commit failure")
	}
	t.committed = true
	return ctx.Err()
}
func (t *negativeCatalogTx) Rollback(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		return errors.New("unbounded cleanup")
	}
	t.cleaned = true
	return nil
}
func TestNegativeCatalogCommitsBeforeReturningLocations(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Pin NegativeQualificationPin }
	if json.Unmarshal(raw, &f) != nil {
		t.Fatal("fixture")
	}
	for _, mode := range []string{"ok", "version", "bucket", "commit", "role"} {
		tenant := uuid.New()
		tx := &negativeCatalogTx{negativeLockTx: negativeLockTx{tenant: tenant, mode: mode}}
		for i, ref := range []struct {
			ID   uuid.UUID
			SHA  string
			Name string
		}{{f.Pin.Calibration.ArtifactID, f.Pin.Calibration.SHA256, "NegativeCalibration"}, {f.Pin.Report.ArtifactID, f.Pin.Report.SHA256, "QualificationReport"}} {
			tx.locations = append(tx.locations, NegativeQualificationObject{Contract: ref.Name, ArtifactID: ref.ID, SHA256: ref.SHA, Bucket: "bucket", Key: ref.Name, Version: "v1", Bytes: int64(100 + i)})
		}
		if mode == "version" {
			tx.locations[0].Version = "null"
		}
		if mode == "bucket" {
			tx.locations[0].Bucket = "other"
		}
		bucket, report, cal, err := (SQLNegativeQualificationCatalog{DB: negativeCatalogDB{tx}, Bucket: "bucket"}).ResolveNegativeQualification(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, uuid.New(), f.Pin)
		if !tx.cleaned {
			t.Fatal("cleanup not bounded")
		}
		if mode == "ok" {
			if err != nil || !tx.committed || bucket != "bucket" || report.SHA256 != f.Pin.Report.SHA256 || cal.SHA256 != f.Pin.Calibration.SHA256 {
				t.Fatal(err)
			}
		} else {
			if err == nil || bucket != "" || report.Key != "" || cal.Key != "" {
				t.Fatal(mode, err)
			}
		}
	}
}
