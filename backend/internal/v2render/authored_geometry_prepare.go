package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// SQLAuthoredGeometryPreparation is the concrete blend preparation path. Native
// absence remains Pending: this object does not relabel ordinary outputs or
// manufacture a native qualification, receipt or succeeded result.
type SQLAuthoredGeometryPreparation struct {
	Scheduler        *pgxpool.Pool
	API              *pgxpool.Pool
	Objects          v2input.SessionAnalysisObjectReader
	PublisherObjects artifactjson.Objects
	Bucket           string
	Native           *SQLAuthoredNativePreparation
	Generation       v2input.Ref // explicit operator-selected actual versioned evidence
}

func (s SQLAuthoredGeometryPreparation) Prepare(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredGeometryTaskPlan, error) {
	zero := AuthoredGeometryTaskPlan{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if s.Scheduler == nil || s.API == nil || s.Objects == nil || s.PublisherObjects == nil || s.Bucket == "" || !validLayerArtifactRef(s.Generation) {
		return zero, renderUnavailable()
	}
	prefix, found, e := ReadAuthoredGeometryPrefix(ctx, s.Scheduler, tenant, parent, ordinal)
	if e != nil {
		return zero, e
	}
	if !found {
		return zero, v2input.ErrExpired
	}
	source := prefix.Context
	if who.UserID != source.Actor || who.SessionID != source.Session {
		return zero, v2input.ErrInvalid
	}
	generation, e := (SQLAuthoredGeometryGenerationReader{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}).Read(ctx, who, source, s.Generation)
	if e != nil {
		return zero, e
	}
	native, e := (SQLAuthoredGeometryNativeReader{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}).Read(ctx, source)
	if errors.Is(e, ErrAuthoredGeometryPending) && s.Native != nil {
		if s.Native.Scheduler != s.Scheduler || s.Native.API != s.API || s.Native.Bucket != s.Bucket {
			return zero, v2input.ErrInvalid
		}
		if _, prepareErr := s.Native.Prepare(ctx, who, tenant, parent, ordinal); prepareErr != nil {
			return zero, prepareErr
		}
		// A genuinely admitted child is still not a canonical adopted producer.
		// Yield to ordinary worker/gateway adoption; the next poll re-reads it.
		return zero, ErrAuthoredGeometryPending
	}
	if e != nil {
		return zero, e
	}
	witnesses, parallel, e := (SQLAuthoredGeometryWitnessReader{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}).Read(ctx, prefix)
	if e != nil {
		return zero, e
	}
	compiled, e := compileAuthoredGeometryPreparationParams(prefix, parallel, native)
	if e != nil {
		return zero, e
	}
	ref, e := (SQLAuthoredGeometryParamsPublication{Scheduler: s.Scheduler, Objects: s.PublisherObjects, Bucket: s.Bucket}).Publish(ctx, source, compiled, witnesses)
	if e != nil {
		return zero, e
	}
	phaseObjects := SQLAuthoredGeometryPhaseObjects{API: s.API, Objects: s.Objects, Bucket: s.Bucket}
	evidence, e := ReadAuthoredGeometryInputEvidence(ctx, s.Scheduler, phaseObjects, who, prefix, compiled, ref, witnesses)
	if e != nil {
		return zero, e
	}
	image, e := review.ParseWorkingImage(native.documents[native.witness.Image])
	if e != nil {
		return zero, e
	}
	profile, e := CompileVerifiedAuthoredGeometryExecutionProfile(ctx, generation, image.Canvas.Width, image.Canvas.Height, image.Color.ICCSHA256)
	if e != nil {
		return zero, e
	}
	plan, e := compileAuthoredGeometryTask(evidence, ref, profile)
	if e != nil {
		return zero, e
	}
	// Repeat actual generation/qualification/current source after all closure IO.
	again, e := (SQLAuthoredGeometryGenerationReader{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}).Read(ctx, who, source, s.Generation)
	if e != nil {
		return zero, e
	}
	if again.documentJCS != generation.documentJCS || again.reportJCS != generation.reportJCS {
		return zero, v2input.ErrExpired
	}
	if e = (SQLAuthoredGeometryAdmission{Scheduler: s.Scheduler}).Admit(ctx, source, plan, witnesses, s.Bucket, s.PublisherObjects); e != nil {
		return zero, e
	}
	if e = ctx.Err(); e != nil {
		return zero, e
	}
	return plan, nil
}
