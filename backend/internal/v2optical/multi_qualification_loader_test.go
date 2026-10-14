package v2optical

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
)

type multiEvidenceIO struct {
	negativeEvidenceIO
	operation string
}

func (s *multiEvidenceIO) ResolveMultiQualification(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, pin MultiQualificationPin, operation string) (string, objectstore.Object, objectstore.Object, error) {
	s.operation = operation
	return s.negativeEvidenceIO.ResolveNegativeQualification(ctx, who, tenant, project, NegativeQualificationPin(pin))
}
func TestMultiQualificationLoaderReadsExactOriginalVersions(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, mode := range []string{"ok", "changed", "corrupt", "oversize"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
				store := &multiEvidenceIO{negativeEvidenceIO: negativeEvidenceIO{r: report, c: calibration, mode: mode,
					report: objectstore.Object{Key: "report", VersionID: "v1", SHA256: pin.Report.SHA256, Bytes: int64(len(report)), ContentType: "application/json"},
					cal:    objectstore.Object{Key: "cal", VersionID: "v1", SHA256: pin.Calibration.SHA256, Bytes: int64(len(calibration)), ContentType: "application/json"}}}
				located, err := LoadMultiQualificationEvidence(context.Background(), store, store, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), pin, locks, operation, params, originals)
				if mode == "ok" {
					if err != nil {
						t.Fatal(err)
					}
					objects, err := located.Objects()
					if err != nil || len(objects) != 2 || objects[0].Contract != "Calibration" || objects[1].Contract != "Report" || store.catalogs < 6 || store.operation != operation {
						t.Fatal("missing original evidence/checkpoints", err)
					}
					for _, stale := range []bool{false, true} {
						tenant := uuid.New()
						tx := &multiLocatedTx{negativeLockTx: negativeLockTx{tenant: tenant}, operation: operation, stale: stale}
						lockErr := LockLocatedMultiQualification(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, uuid.New(), located)
						if !tx.observed || (!stale && lockErr != nil) || (stale && lockErr != v2input.ErrExpired) {
							t.Fatal("exact locator lock", lockErr)
						}
					}
					objects[0].Version = "mutated"
					copied, err := located.Objects()
					if err != nil || copied[0].Version != "v1" {
						t.Fatal("metadata aliases caller")
					}
					params[0] = ' '
					originals[0][0] = ' '
					report[0] = ' '
					if _, err := located.Qualification(); err != nil {
						t.Fatal("evidence aliases caller", err)
					}
				} else if err == nil || store.reads != 1 {
					t.Fatal("substituted stream accepted", err)
				}
				if store.reads != store.closes {
					t.Fatal("unclosed evidence stream")
				}
			})
		}
	}
}

type multiLocatedTx struct {
	negativeLockTx
	operation string
	stale     bool
	observed  bool
}

func (t *multiLocatedTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if args[0] != t.tenant {
			return v2input.ErrInvalid
		}
		switch q {
		case "SELECT photo.lock_v29_multi_qualification($1,$2,$3,$4,$5::jsonb,$6)":
			if len(args) != 6 || args[5] != t.operation {
				return v2input.ErrInvalid
			}
			*dest[0].(*bool) = true
		case "SELECT photo.lock_v29_multi_qualification_objects($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7)":
			var objects []PerspectiveQualificationObject
			if len(args) != 7 || args[6] != t.operation || json.Unmarshal([]byte(args[5].(string)), &objects) != nil || len(objects) != 2 || objects[0].Version != "v1" {
				return v2input.ErrInvalid
			}
			t.observed = true
			*dest[0].(*bool) = !t.stale
		default:
			return v2input.ErrInvalid
		}
		return nil
	})
}
