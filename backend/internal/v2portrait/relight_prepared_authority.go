package v2portrait

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

// relightPreparedAuthority retains independently verified original object
// locators, including both selectors when a manual root is shared. Public
// content-only compiler results cannot manufacture this witness.
type relightPreparedAuthority struct {
	depthBody, subjectBody, protectedBody string
	bucket                                string
	request                               RelightBootstrapRequest
	params                                RelightParams
	intent                                RelightIntent
	depth, person                         RelightPreintentProducer
	manuals                               []RelightManualProducer
}

func newRelightPreparedAuthority(bucket string, r RelightBootstrapRequest, p RelightParams, intent RelightIntent, depth, person RelightPreintentProducer, manuals []RelightManualProducer, documents RelightInputDocuments) *relightPreparedAuthority {
	// Only scalar request scope is needed after verification. Retain no mutable
	// public document/evidence buffers or deployment maps.
	r.ParamsRaw, r.SourceRaw = nil, nil
	r.Evidence = RelightQualificationEvidence{}
	w := &relightPreparedAuthority{depthBody: string(documents.Depth), subjectBody: string(documents.Subject), protectedBody: string(documents.Protected), bucket: bucket, request: r, params: p, intent: intent, depth: depth, person: person}
	for _, producer := range manuals {
		copyProducer := RelightManualProducer{RootBody: append([]byte(nil), producer.RootBody...)}
		if producer.preintent != nil {
			copyWitness := *producer.preintent
			copyWitness.objects = make(map[string]relightProducerLocator, len(producer.preintent.objects))
			for kind, locator := range producer.preintent.objects {
				copyWitness.objects[kind] = locator
			}
			copyProducer.preintent = &copyWitness
		}
		w.manuals = append(w.manuals, copyProducer)
	}
	return w
}

// RevalidatePrepared retains current source, qualification and ALL original
// producer/dependency locks in the supplied scheduler transaction. It replays
// the exact saved intent and performs no object IO or commit. A future dedicated
// reservation must execute in this same transaction; committing this check and
// reserving separately would reopen a revocation race. This is not a quota,
// publication, task admission or durable recovery grant.
func (s RelightBootstrapCoordinator) RevalidatePrepared(ctx context.Context, tx pgx.Tx, result RelightBootstrapResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w := result.prepared
	if tx == nil || w == nil || s.Bucket == "" || s.Bucket != w.bucket || result.Intent != w.intent {
		return v2input.ErrInvalid
	}
	r, p := w.request, w.params
	if err := s.authority(ctx, tx, r, p); err != nil {
		return err
	}
	current, err := DiscoverRelightPreintentProducer(ctx, tx, r.Who, r.Tenant, r.Project, r.Snapshot, p.Base, p.Depth, "depth", s.Bucket)
	if err != nil {
		return err
	}
	if current != w.depth {
		return v2input.ErrExpired
	}
	if r.SubjectKind == "person" {
		current, err = DiscoverRelightPreintentProducer(ctx, tx, r.Who, r.Tenant, r.Project, r.Snapshot, p.Base, p.Subject, "person", s.Bucket)
		if err != nil {
			return err
		}
		if current != w.person {
			return v2input.ErrExpired
		}
	}
	for _, manual := range w.manuals {
		if err = s.checkManual(ctx, tx, r, p, manual); err != nil {
			return err
		}
	}
	return SaveRelightIntent(ctx, tx, r.Tenant, r.Run, w.intent)
}
