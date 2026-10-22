package v2render

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

func NewSQLAuthoredFinalContinuation(scheduler, api *pgxpool.Pool, objects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}, bucket string, catalog *PinnedAuthoredFinalCalibrationCatalog) (*SQLAuthoredFinalContinuation, error) {
	if scheduler == nil || api == nil || objects == nil || bucket == "" || catalog == nil {
		return nil, v2input.ErrInvalid
	}
	phase := SQLAuthoredPhaseObjects{API: api, Objects: objects, Bucket: bucket}
	publication := AuthoredContinuationPublication{Scheduler: scheduler, PhaseObjects: phase, Objects: objects, Bucket: bucket}
	return &SQLAuthoredFinalContinuation{Preparation: SQLAuthoredFinalPreparation{API: api, Params: AuthoredFinalParamsPublication{Continuation: publication}, Calibrations: catalog}, Producers: AuthoredFinalProducerReader{Scheduler: scheduler, Objects: objects, Bucket: bucket, PhaseObjects: phase}, Objects: objects}, nil
}
