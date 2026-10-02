package v2input

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// LensRuntime keeps authorized API reads and scheduler mutations on distinct
// role pools. Registry bytes must have been loaded from operator configuration.
type LensRuntime struct {
	API, Scheduler *pgxpool.Pool
	Objects        *objectstore.Store
	Bucket         string
	Registry       LensRegistryConfiguration
}

func (r LensRuntime) Coordinator(ctx context.Context) (LensAdmissionCoordinator, error) {
	if r.API == nil || r.Scheduler == nil || r.API == r.Scheduler || r.Objects == nil || r.Bucket == "" || !digestPattern.MatchString(r.Registry.RuntimeLockSHA) || len(r.Registry.Pins) < 1 || len(r.Registry.Pins) > 10000 {
		return LensAdmissionCoordinator{}, ErrInvalid
	}
	for pool, expected := range map[*pgxpool.Pool]string{r.API: "photo_api", r.Scheduler: "photo_scheduler"} {
		var role string
		if err := pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return LensAdmissionCoordinator{}, err
		}
		if role != expected {
			return LensAdmissionCoordinator{}, ErrInvalid
		}
	}
	pins := append([]LensQualificationPin(nil), r.Registry.Pins...)
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: r.Scheduler, Bucket: r.Bucket}, Objects: r.Objects}
	store := SQLLensDraftStore{DB: r.Scheduler}
	return LensAdmissionCoordinator{
		Selection: SQLLensSelectionSource{DB: r.API, Objects: r.Objects, Bucket: r.Bucket},
		Profiles:  SQLLensProfileSource{DB: r.API, Objects: r.Objects, Bucket: r.Bucket, RuntimeLockSHA: r.Registry.RuntimeLockSHA, QualificationPins: append([]LensQualificationPin(nil), pins...)},
		Drafts:    LensDraftCoordinator{Store: store, Preparation: LensPreparationCoordinator{Documents: documents, Stage: SQLLensTaskStage{DB: r.Scheduler}, Inputs: Admission{DB: r.Scheduler}}},
		Sealer:    store, RuntimeLockSHA: r.Registry.RuntimeLockSHA, QualificationPins: pins,
	}, nil
}
