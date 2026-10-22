package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

// This catalog owns only an already physically verified NEW generation. It
// cannot qualify an old measured catalog entry by changing its contracts pin.
type verifiedFinalGenerationCatalog struct {
	proof VerifiedAuthoredFinalGeneration
}

func (c verifiedFinalGenerationCatalog) ResolveAuthoredFinal(ctx context.Context, q RenderQualification) (RenderCalibration, error) {
	if e := ctx.Err(); e != nil {
		return RenderCalibration{}, e
	}
	d, e := c.proof.Evidence()
	if e != nil || d.Qualification != q {
		return RenderCalibration{}, renderUnavailable()
	}
	return d.Calibration, nil
}

// The deployment supplies an actual immutable artifact reference, not a passed
// flag. Every Prepare (including Resume's draft branch) physically verifies it.
func NewSQLAuthoredFinalRoot3Continuation(scheduler, api *pgxpool.Pool, objects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}, bucket string, generation v2input.Ref) (*SQLAuthoredFinalContinuation, error) {
	if scheduler == nil || api == nil || objects == nil || bucket == "" || !authoredGenerationRefValid(generation) {
		return nil, v2input.ErrInvalid
	}
	phase := SQLAuthoredPhaseObjects{API: api, Objects: objects, Bucket: bucket}
	publication := AuthoredContinuationPublication{Scheduler: scheduler, PhaseObjects: phase, Objects: objects, Bucket: bucket}
	reader := &SQLAuthoredFinalGenerationReader{Scheduler: scheduler, Objects: objects, Bucket: bucket}
	return &SQLAuthoredFinalContinuation{Preparation: SQLAuthoredFinalPreparation{API: api, Params: AuthoredFinalParamsPublication{Continuation: publication}, Generation: reader, GenerationRef: &generation}, Producers: AuthoredFinalProducerReader{Scheduler: scheduler, Objects: objects, Bucket: bucket, PhaseObjects: phase}, Objects: objects}, nil
}

// Each tenant has its own physically authenticated artifact. Own the map so a
// deployment caller cannot mutate selection while Prepare is doing object IO.
func NewSQLAuthoredFinalRoot3TenantContinuation(scheduler, api *pgxpool.Pool, objects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}, bucket string, generations map[uuid.UUID]v2input.Ref) (*SQLAuthoredFinalContinuation, error) {
	if len(generations) < 1 || len(generations) > 256 {
		return nil, v2input.ErrInvalid
	}
	owned := map[uuid.UUID]v2input.Ref{}
	var first v2input.Ref
	for tenant, ref := range generations {
		if tenant == uuid.Nil || !authoredGenerationRefValid(ref) {
			return nil, v2input.ErrInvalid
		}
		owned[tenant] = ref
		first = ref
	}
	result, e := NewSQLAuthoredFinalRoot3Continuation(scheduler, api, objects, bucket, first)
	if e != nil {
		return nil, e
	}
	result.Preparation.GenerationRef = nil
	result.Preparation.generationRefs = owned
	return result, nil
}
