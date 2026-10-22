package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"photoeditor/backend/internal/v2qualification"
)

// Deployment is selected by the server/operator, never from original Params,
// a client request or report claims. Missing policies are explicitly unavailable.
type AuthoredNativeMultiDeployment struct {
	Pin      v2optical.MultiQualificationPin
	Locks    v2qualification.DeploymentLocks
	Capacity v2optical.MultiResourceEstimate
}
type SQLAuthoredNativePreparation struct {
	Scheduler, API   *pgxpool.Pool
	Objects          v2input.SessionAnalysisObjectReader
	PublisherObjects artifactjson.Objects
	Bucket           string
	UpscaleExpected  v2qualification.Expected
	UpscaleReport    v2input.Ref
	Panorama, Focus  *AuthoredNativeMultiDeployment
}

func (s SQLAuthoredNativePreparation) Prepare(ctx context.Context, who auth.Identity, tenant, parent uuid.UUID, ordinal int) (AuthoredNativeTaskPlan, error) {
	zero := AuthoredNativeTaskPlan{}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if s.Scheduler == nil || s.API == nil || s.Objects == nil || s.PublisherObjects == nil || s.Bucket == "" {
		return zero, renderUnavailable()
	}
	phase := SQLAuthoredGeometryPhaseObjects{API: s.API, Objects: s.Objects, Bucket: s.Bucket}
	materializer := SQLAuthoredNativeMaterializer{Scheduler: s.Scheduler, Objects: phase}
	m, e := materializer.Read(ctx, who, tenant, parent, ordinal)
	if e != nil {
		return zero, e
	}
	catalog, e := NewSQLAuthoredGeometryInputCatalog(ctx, s.Scheduler, phase, who, tenant, parent, ordinal)
	if e != nil {
		return zero, e
	}
	sources, e := readAuthoredNativeSourceDocuments(ctx, who, m, catalog)
	if e != nil {
		return zero, e
	}
	params, e := (SQLAuthoredNativeParamsPublication{Scheduler: s.Scheduler, Objects: s.PublisherObjects, Bucket: s.Bucket}).Publish(ctx, m)
	if e != nil {
		return zero, e
	}
	var plan AuthoredNativeTaskPlan
	switch m.selected.Operation {
	case "image.upscale":
		request, e := authoredNativeQualificationRequest(m)
		if e != nil {
			return zero, e
		}
		qualifier := SQLAuthoredGeometryNativeQualification{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}
		q, e := qualifier.Read(ctx, tenant, request, s.UpscaleExpected, s.UpscaleReport)
		if e != nil {
			return zero, e
		}
		inputs, e := readAuthoredNativeInputs(ctx, who, m, params, catalog, nil)
		if e != nil {
			return zero, e
		}
		// Use the actual scoped versioned object sizes, never caller estimates.
		seen := map[uuid.UUID]bool{}
		var size int64
		for _, o := range inputs.Occurrences {
			if !seen[o.Node.Ref.ArtifactID] {
				if o.Object.Bytes < 1 || o.Object.Bytes > 9007199254740991-size {
					return zero, v2input.ErrInvalid
				}
				seen[o.Node.Ref.ArtifactID] = true
				size += o.Object.Bytes
			}
		}
		if len(sources) != 1 {
			return zero, v2input.ErrInvalid
		}
		plan, e = compileAuthoredUpscaleProposal(ctx, m, params, inputs, sources[0], size, q)
		if e != nil {
			return zero, e
		}
		again, e := qualifier.Read(ctx, tenant, request, s.UpscaleExpected, s.UpscaleReport)
		if e != nil {
			return zero, e
		}
		if q.object != again.object || q.expected != again.expected || q.reportJCS != again.reportJCS {
			return zero, v2input.ErrExpired
		}
	case "optical.panorama", "optical.focus_stack":
		deployment := s.Panorama
		if m.selected.Operation == "optical.focus_stack" {
			deployment = s.Focus
		}
		if deployment == nil {
			return zero, renderUnavailable()
		}
		// Own every deployment collection before physical IO invokes a dependency.
		raw, e := canonical.Marshal(deployment)
		if e != nil {
			return zero, e
		}
		var own AuthoredNativeMultiDeployment
		if decodeAuthoredParams(raw, &own) != nil {
			return zero, v2input.ErrInvalid
		}
		qcatalog := v2optical.SQLMultiQualificationCatalog{DB: s.Scheduler, Bucket: s.Bucket}
		located, e := v2optical.LoadMultiQualificationEvidence(ctx, qcatalog, s.Objects, who, tenant, m.source.Project, own.Pin, own.Locks, m.selected.Operation, m.selected.Params.Body, sources)
		if e != nil {
			return zero, e
		}
		qualified, e := located.Qualification()
		if e != nil {
			return zero, e
		}
		inputs, e := readAuthoredNativeInputs(ctx, who, m, params, catalog, &authoredNativeMultiInputs{pin: own.Pin, catalog: qcatalog})
		if e != nil {
			return zero, e
		}
		plan, e = compileAuthoredMultiProposal(ctx, m, params, inputs, sources, qualified, own.Capacity)
		if e != nil {
			return zero, e
		}
		// Re-read the real policy and original report/calibration versions after the
		// complete closure has been traversed and before staging the child.
		again, e := v2optical.LoadMultiQualificationEvidence(ctx, qcatalog, s.Objects, who, tenant, m.source.Project, own.Pin, own.Locks, m.selected.Operation, m.selected.Params.Body, sources)
		if e != nil {
			return zero, e
		}
		before, e := located.Objects()
		if e != nil {
			return zero, e
		}
		after, e := again.Objects()
		if e != nil {
			return zero, e
		}
		a, e := canonical.Marshal(before)
		if e != nil {
			return zero, e
		}
		b, e := canonical.Marshal(after)
		if e != nil || !bytes.Equal(a, b) {
			return zero, v2input.ErrExpired
		}
	default:
		return zero, v2input.ErrInvalid
	}
	current, e := materializer.Read(ctx, who, tenant, parent, ordinal)
	if e != nil {
		return zero, e
	}
	if !bytes.Equal(current.proof, m.proof) || !bytes.Equal(current.selected.Params.Body, m.selected.Params.Body) {
		return zero, v2input.ErrExpired
	}
	if e = (SQLAuthoredNativeAdmission{Scheduler: s.Scheduler}).Admit(ctx, m, plan, s.Bucket, s.PublisherObjects); e != nil {
		return zero, e
	}
	return plan, ctx.Err()
}
