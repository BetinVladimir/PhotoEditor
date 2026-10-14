package v2optical

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

type multiCapabilitiesTx struct {
	perspectiveArrayTx
	operation string
	ready     bool
}

func (t *multiCapabilitiesTx) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT current_user" {
		return t.negativeLockTx.QueryRow(ctx, q, args...)
	}
	return negativeLockRow(func(dest ...any) error {
		if q != "SELECT photo.v29_multi_capabilities_ready($1,$2,$3,$4,$5::jsonb,$6)" || len(args) != 6 || args[0] != t.tenant || args[5] != t.operation {
			return v2input.ErrInvalid
		}
		*dest[0].(*bool) = t.ready
		return nil
	})
}

type multiCapabilitiesDB struct{ tx *multiCapabilitiesTx }

func (d multiCapabilitiesDB) Begin(context.Context) (pgx.Tx, error) { return d.tx, nil }
func TestMultiCapabilitiesRequireCurrentAuthority(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		for _, ready := range []bool{false, true} {
			pin, _, _, _, _, _ := multiQualifiedFixture(t, operation)
			tenant, project := uuid.New(), uuid.New()
			tx := &multiCapabilitiesTx{perspectiveArrayTx: perspectiveArrayTx{negativeLockTx: negativeLockTx{tenant: tenant}}, operation: operation, ready: ready}
			service := &MultiService{tenant: tenant, project: project, coordinator: MultiCoordinator{DB: multiCapabilitiesDB{tx}, Operation: operation, Pin: pin, MaxPixels: 200000000, Capacity: MultiResourceEstimate{PixelCount: 10000}}}
			caps, err := service.describe(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tenant, project)
			if ready {
				if err != nil || !tx.committed || caps.Calibration != pin.Calibration || caps.MaxPixels != 10000 || caps.Operation != operation {
					t.Fatal("invalid capabilities", err)
				}
			} else if err != v2qualification.ErrUnqualified || tx.committed {
				t.Fatal("disabled authority advertised", err)
			}
		}
	}
}
