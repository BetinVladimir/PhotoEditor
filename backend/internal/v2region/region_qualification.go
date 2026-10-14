package v2region

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
)

// SQLRegionQualificationResolver is installation-owned. Its calibration/evidence
// pins must be loaded with the independently approved Region catalog, never from
// an HTTP request. Queue preparation proves editor authority; this resolver
// supplies scope-bound live qualification only, not user authorization.
type SQLRegionQualificationResolver struct {
	API            *pgxpool.Pool
	CalibrationSHA string
	EvidenceSHA    string
}

func (s SQLRegionQualificationResolver) ResolveRegionQualification(ctx context.Context, plan FrozenRegionPlan) (RegionQualification, error) {
	if err := ctx.Err(); err != nil {
		return RegionQualification{}, err
	}
	if s.API == nil || !validRegionSHA(s.CalibrationSHA) || !validRegionSHA(s.EvidenceSHA) {
		return RegionQualification{}, ErrRegionUnavailable
	}
	if _, err := plan.SourceShape(); err != nil {
		return RegionQualification{}, err
	}
	doc, err := plan.decode()
	if err != nil || doc.TenantID == uuid.Nil || doc.ProjectID == uuid.Nil {
		return RegionQualification{}, ErrRegionInvalid
	}
	var q RegionQualification
	err = database.TenantTransaction(ctx, s.API, doc.TenantID, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return ErrRegionUnavailable
		}
		return tx.QueryRow(ctx, `SELECT * FROM photo.resolve_v26_region_qualification($1,$2,$3,$4)`, doc.TenantID, doc.ProjectID, s.CalibrationSHA, s.EvidenceSHA).
			Scan(&q.RuntimeSHA, &q.ModelSHA, &q.ICCSHA, &q.DeviceSHA, &q.DatasetSHA, &q.PolicySHA, &q.Backend)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RegionQualification{}, ErrRegionUnavailable
	}
	if err != nil {
		return RegionQualification{}, err
	}
	if err := ctx.Err(); err != nil {
		return RegionQualification{}, err
	}
	if !q.valid() || q.ModelSHA != doc.FrozenRecipe.ModelLockSHA || q.ICCSHA != doc.FrozenRecipe.ICCLockSHA {
		return RegionQualification{}, ErrRegionUnavailable
	}
	return q, nil
}

var _ RegionQualificationResolver = SQLRegionQualificationResolver{}

// NewSQLRegionProfileProvider binds live SQL approval to the actual immutable
// catalog digest. A deployment bundle shares one independent evidence artifact;
// heterogeneous evidence bundles require separate providers, not an unchecked
// evidence override. Neither digest nor evidence is accepted from public input.
func NewSQLRegionProfileProvider(api *pgxpool.Pool, catalog *PinnedRegionCalibrationCatalog, evidenceSHA string) (*CompiledRegionProfiles, error) {
	if api == nil || catalog == nil || !validRegionSHA(catalog.manifestSHA) || !validRegionSHA(evidenceSHA) || len(catalog.profiles) == 0 {
		return nil, ErrRegionUnavailable
	}
	for _, profile := range catalog.profiles {
		if profile.EvidenceSHA != evidenceSHA {
			return nil, ErrRegionUnavailable
		}
	}
	resolver := SQLRegionQualificationResolver{API: api, CalibrationSHA: catalog.manifestSHA, EvidenceSHA: evidenceSHA}
	return &CompiledRegionProfiles{Qualifier: CalibratedRegionCosts{Resolver: resolver, Catalog: catalog}}, nil
}
