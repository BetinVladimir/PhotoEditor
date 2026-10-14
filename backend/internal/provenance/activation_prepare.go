package provenance

import (
	"bytes"
	"context"
	"time"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

// Implementations must measure/qualify the pinned source and installed signing
// runtime, including decoded image peak RAM, TSA/verification and scratch.
// A caller-provided profile or protocol floor is not resource qualification.
type SigningResourceQualifier interface {
	QualifySigningResources(context.Context, auth.Identity, FrozenSigningIntent, SigningExportEvidence) (SigningTaskResources, error)
}
type SigningActivationPreparation struct {
	Source    SigningExportSource
	Registry  *SigningPolicyRegistry
	Resources SigningResourceQualifier
	Clock     func() time.Time
}

// Private owned receipt for later SQL admission. This is not a runnable ticket.
type PreparedSigningActivation struct {
	intent   FrozenSigningIntent
	compiled FrozenSigningTask
	mime     string
}

func (p PreparedSigningActivation) Task() FrozenSigningTask { return cloneSigningTask(p.compiled) }
func cloneSigningIntent(i FrozenSigningIntent) FrozenSigningIntent {
	i.params = append([]byte(nil), i.params...)
	i.lineage = append([]byte(nil), i.lineage...)
	i.request = append([]byte(nil), i.request...)
	return i
}
func cloneSigningTask(p FrozenSigningTask) FrozenSigningTask {
	return FrozenSigningTask{resources: p.resources, task: p.Task(), params: p.params, body: append([]byte(nil), p.body...), inputs: p.Inputs()}
}
func signingEvidenceMatches(i FrozenSigningIntent, e SigningExportEvidence) bool {
	params, err := DecodeSignParams(i.params)
	return err == nil && e.Project == i.project && e.ExportID == i.export && e.Export == params.Export && e.Lineage == params.Lineage && bytes.Equal(e.LineageBody, i.lineage)
}
func (p SigningActivationPreparation) Prepare(ctx context.Context, who auth.Identity, intent FrozenSigningIntent, expected FrozenSigningTask) (PreparedSigningActivation, error) {
	var zero PreparedSigningActivation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Source == nil || p.Registry == nil || p.Resources == nil || p.Clock == nil || who.UserID != intent.actor || who.SessionID != intent.session {
		return zero, ErrInvalid
	}
	owned := cloneSigningIntent(intent)
	evidence, err := p.Source.ResolveSigningExport(ctx, who, owned.tenant, owned.export)
	if err != nil {
		return zero, err
	}
	if !signingEvidenceMatches(owned, evidence) {
		return zero, ErrInvalid
	}
	evidence.LineageBody = append([]byte(nil), evidence.LineageBody...)
	// Check the task and live policy before invoking native qualification.
	preflight, err := CompileSigningTask(ctx, owned, p.Registry, evidence.MIME, expected.resources, p.Clock())
	if err != nil {
		return zero, err
	}
	preflightBytes, err := canonical.Marshal(preflight.Task())
	if err != nil {
		return zero, err
	}
	expectedBytes, err := canonical.Marshal(expected.Task())
	if err != nil || !bytes.Equal(preflightBytes, expectedBytes) {
		return zero, ErrInvalid
	}
	callback := evidence
	callback.LineageBody = append([]byte(nil), evidence.LineageBody...)
	resources, err := p.Resources.QualifySigningResources(ctx, who, cloneSigningIntent(owned), callback)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	compiled, err := CompileSigningTask(ctx, owned, p.Registry, evidence.MIME, resources, p.Clock())
	if err != nil {
		return zero, err
	}
	want, err := canonical.Marshal(expected.Task())
	if err != nil {
		return zero, err
	}
	actual, err := canonical.Marshal(compiled.Task())
	if err != nil || !bytes.Equal(want, actual) {
		return zero, ErrInvalid
	}
	// Never trust callback-mutated or stale source evidence after qualification.
	current, err := p.Source.ResolveSigningExport(ctx, who, owned.tenant, owned.export)
	if err != nil {
		return zero, err
	}
	if !signingEvidenceMatches(owned, current) || current.MIME != evidence.MIME {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PreparedSigningActivation{intent: owned, compiled: cloneSigningTask(compiled), mime: evidence.MIME}, nil
}
