package v2render

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// AuthoredFaceCandidatePublication retains three actual canonical producer
// observations. Publishing a candidate neither accepts faces nor completes any
// warning-bearing producer or parent run.
type AuthoredFaceCandidatePublication struct {
	Producers    AuthoredFaceProducerReader
	PhaseObjects SQLAuthoredPhaseObjects
}

func (p AuthoredFaceCandidatePublication) Publish(ctx context.Context, who auth.Identity, source AuthoredInstructionSource) (AuthoredFaceSelectionCandidate, string, error) {
	var zero AuthoredFaceSelectionCandidate
	if err := ctx.Err(); err != nil {
		return zero, "", err
	}
	if who.UserID != source.Actor || who.SessionID != source.Session || p.Producers.Scheduler == nil {
		return zero, "", v2input.ErrInvalid
	}
	image, err := p.PhaseObjects.ReadJSON(ctx, who, source.Tenant, source.Project, source.Parent, source.Instruction.Ordinal, source.Source.Source)
	if err != nil {
		return zero, "", err
	}
	ops := []string{"face.detect", "face.landmarks", "face.parse"}
	observations := make([]currentAuthoredFaceProducer, len(ops))
	for i, op := range ops {
		observations[i], err = p.Producers.read(ctx, source, op)
		if err != nil {
			return zero, "", err
		}
	}
	candidate, body, err := CompileAuthoredFaceSelectionCandidate(source, image, observations[0].root, observations[1].root, observations[2].root, observations[0].observation, observations[1].observation, observations[2].observation)
	if err != nil {
		return zero, "", err
	}
	// No accepted decision is copied from any prior source. Reacquire each
	// physical observation before the SQL current-candidate predicate locks and
	// atomically inserts this exact canonical body.
	for i, op := range ops {
		current, e := p.Producers.read(ctx, source, op)
		if e != nil {
			return zero, "", e
		}
		if !bytes.Equal(current.proof, observations[i].proof) || !bytes.Equal(current.root, observations[i].root) || !bytes.Equal(current.params, observations[i].params) {
			return zero, "", v2input.ErrExpired
		}
	}
	var sha *string
	err = database.TenantTransaction(ctx, p.Producers.Scheduler, source.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.publish_authored_face_selection_candidate($1,$2,$3,$4)", source.Tenant, source.Parent, source.Instruction.Ordinal, string(body)).Scan(&sha)
	})
	if err != nil {
		return zero, "", err
	}
	if sha == nil {
		return zero, "", v2input.ErrExpired
	}
	if *sha != authoredFaceCandidateSHA(body) {
		return zero, "", v2input.ErrInvalid
	}
	return candidate, *sha, ctx.Err()
}
