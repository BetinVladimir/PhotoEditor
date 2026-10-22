package semanticreview

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InstallExposureApplyService is the database-verified installation entry point.
// NewExposureApplyService remains the pure composition constructor.
func InstallExposureApplyService(ctx context.Context, config ExposureApplyInstallation) (*ApplyService, error) {
	service, err := NewExposureApplyService(config)
	if err != nil {
		return nil, err
	}
	var role string
	if err = config.API.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return nil, err
	}
	if role != "photo_api" {
		return nil, ErrUnavailable
	}
	if err = VerifyExposureAdmissionRole(ctx, config.Admission); err != nil {
		return nil, err
	}
	if config.ShownRetouchExecution != nil {
		if err = verifyShownRetouchAdmissionRole(ctx, config.Admission); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// ExposureApplyInstallation is deployment-owned configuration. Admission must
// be a separately provisioned trusted transaction contour; API credentials must
// not be granted private admission functions by this constructor.
type ExposureApplyInstallation struct {
	ShownRetouchExecution            *ExposureDeploymentBudget
	ShownRetouchPreview              *RetouchPreviewProvider
	ShownBackgroundCleanupExecution  *ExposureDeploymentBudget
	ShownBackgroundCleanupPreview    *BackgroundCleanupPreviewProvider
	API, Admission                   *pgxpool.Pool
	Objects                          ExposureApplyObjects
	Bucket                           string
	DocumentBudgetBytes              int64
	ReviewModelSHA, ReviewDatasetSHA string
	Color                            ExposureColorProfile
	Execution                        ExposureDeploymentBudget
	ShownResolver                    ShownApplyResolver
	ShownExecution                   *ExposureDeploymentBudget
	ShownPreview                     *ExposurePreviewProvider
	ShownRemovalExecution            *ExposureDeploymentBudget
	ShownRemovalPreview              *RemovalPreviewProvider
}

// NewExposureApplyService assembles concrete scoped readers, rejection/exposure
// preparation and transaction orchestration. It neither verifies scientific
// qualification nor grants privileges or activates operations at startup.
func NewExposureApplyService(config ExposureApplyInstallation) (*ApplyService, error) {
	if config.API == nil || config.Admission == nil || config.Objects == nil {
		return nil, ErrUnavailable
	}
	read, err := NewPlanObjectReader(config.Objects, config.Bucket)
	if err != nil {
		return nil, err
	}
	selectBudgets, err := NewExposureBudgetSelection(config.Execution)
	if err != nil {
		return nil, err
	}
	prepare, err := NewExposurePreparation(config.API, config.Objects, read, config.Bucket, config.DocumentBudgetBytes,
		config.ReviewModelSHA, config.ReviewDatasetSHA, config.Color, selectBudgets)
	if err != nil {
		return nil, err
	}
	if config.ShownExecution != nil {
		if config.ShownResolver != nil || config.ShownPreview == nil {
			return nil, ErrInvalid
		}
		selection, e := NewShownBudgetSelection(*config.ShownExecution)
		if e != nil {
			return nil, e
		}
		config.ShownResolver, e = NewSQLShownApplyResolver(config.API, config.ShownPreview, read, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA, selection)
		if e != nil {
			return nil, e
		}
	}
	var removal, cleanup, retouch ShownApplyResolver
	if config.ShownRemovalExecution != nil {
		if config.ShownRemovalPreview == nil {
			return nil, ErrInvalid
		}
		selection, e := NewShownBudgetSelection(*config.ShownRemovalExecution)
		if e != nil {
			return nil, e
		}
		removal, e = NewSQLShownRemovalApplyResolver(config.API, config.ShownRemovalPreview, read, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA, selection)
		if e != nil {
			return nil, e
		}
	}
	if config.ShownBackgroundCleanupExecution != nil {
		if config.ShownBackgroundCleanupPreview == nil {
			return nil, ErrInvalid
		}
		selection, e := NewShownBudgetSelection(*config.ShownBackgroundCleanupExecution)
		if e != nil {
			return nil, e
		}
		cleanup, e = NewSQLShownBackgroundCleanupApplyResolver(config.API, config.ShownBackgroundCleanupPreview, read, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA, selection)
		if e != nil {
			return nil, e
		}
	}
	if config.ShownRetouchExecution != nil {
		if config.ShownRetouchPreview == nil {
			return nil, ErrInvalid
		}
		selection, e := NewShownBudgetSelection(*config.ShownRetouchExecution)
		if e != nil {
			return nil, e
		}
		retouch, e = NewSQLShownRetouchApplyResolver(config.API, config.ShownRetouchPreview, read, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA, selection)
		if e != nil {
			return nil, e
		}
	}
	if removal != nil || cleanup != nil || retouch != nil {
		config.ShownResolver, err = NewSQLShownOperationResolver(config.API, read, config.DocumentBudgetBytes, config.ReviewModelSHA, config.ReviewDatasetSHA, config.ShownResolver, removal, cleanup, retouch)
		if err != nil {
			return nil, err
		}
	}

	if config.ShownResolver != nil {
		prepare, err = NewShownApplyPreparation(config.API, config.Objects, config.Bucket, prepare, config.ShownResolver)
		if err != nil {
			return nil, err
		}
	}
	return &ApplyService{API: config.API, Admission: config.Admission, Prepare: prepare}, nil
}
