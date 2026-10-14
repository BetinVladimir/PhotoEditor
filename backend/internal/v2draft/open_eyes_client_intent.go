package v2draft

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

const MaxOpenEyesIntentBytes = 65536 + 4096

// OpenEyesClientIntent carries a prepared proposal. Its face IDs and digest are
// untrusted until submit re-derives them and issues the confirmation document.
type OpenEyesClientIntent = OpenEyesClientPreparation

func DecodeOpenEyesClientIntent(raw []byte) (OpenEyesClientIntent, error) {
	var zero OpenEyesClientIntent
	if len(raw) < 2 || len(raw) > MaxOpenEyesIntentBytes {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var intent OpenEyesClientIntent
	if json.Unmarshal(raw, &intent) != nil || intent.SchemaVersion != 1 ||
		intent.Shoot == uuid.Nil || intent.Snapshot == uuid.Nil ||
		intent.Ordinal < 0 || intent.Ordinal >= 5000 ||
		intent.SourceObservation == uuid.Nil || intent.DonorObservation == uuid.Nil ||
		intent.SourceObservation == intent.DonorObservation {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	if _, _, err = intent.Proposal.Compile(); err != nil {
		return zero, v2input.ErrInvalid
	}
	selection := OpenEyesIntentSelection(intent)
	selectionRaw, err := canonical.Marshal(selection)
	if err != nil {
		return zero, err
	}
	if _, err = DecodeOpenEyesClientSelection(selectionRaw); err != nil {
		return zero, err
	}
	return intent, nil
}

// VerifyOpenEyesIntentOwner prevents another key or ordinal from borrowing the
// server-derived confirmation placeholder. It does not establish live consent.
func VerifyOpenEyesIntentOwner(intent OpenEyesClientIntent, tenant, actor uuid.UUID,
	key string) (uuid.UUID, uuid.UUID, error) {
	if tenant == uuid.Nil || actor == uuid.Nil {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run, err := v2input.RunID(tenant, actor, key)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", intent.Ordinal)
	if err != nil || intent.Proposal.Confirmation.ArtifactID != owner {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	return run, workflow, nil
}

// OpenEyesIntentSelection projects only client-selectable fields from a
// proposal; this shape can be re-authorized against current snapshot/catalog.
func OpenEyesIntentSelection(intent OpenEyesClientIntent) OpenEyesClientSelection {
	p := intent.Proposal
	return OpenEyesClientSelection{SchemaVersion: 1, Shoot: intent.Shoot,
		Snapshot: intent.Snapshot, Ordinal: intent.Ordinal, Base: p.Base, Donor: p.Donor,
		Strength: p.Strength, EyesMask: p.EyesMask, Protected: p.Protected,
		TargetPixel: intent.TargetPixel, ProtectedPixel: intent.ProtectedPixel,
		SourceFaces: p.SourceFaces, DonorFaces: p.DonorFaces,
		SourceObservation: intent.SourceObservation, DonorObservation: intent.DonorObservation}
}
