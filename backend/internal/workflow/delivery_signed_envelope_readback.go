package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// ReadSignedDeliveryEnvelope recovers an exact published v2 root under the
// current scheduler lease. SQL repeats policy, closure and physical authority.
// Task registration still needs its own atomic input/activation admission.
func ReadSignedDeliveryEnvelope(ctx context.Context, db artifactjson.RowQuerier,
	lease delivery.ExpansionLease, expected v2input.Ref,
	) (delivery.SignedAssemblyEnvelope, []byte, error) {
	invalid := func() (delivery.SignedAssemblyEnvelope, []byte, error) {
		return delivery.SignedAssemblyEnvelope{}, nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil { return delivery.SignedAssemblyEnvelope{}, nil, err }
	if db == nil || !lease.Valid() || lease.Cursor != lease.SlotCount ||
		expected.ArtifactID == uuid.Nil || domainDigestInvalid(expected.SHA256) { return invalid() }
	var raw, sha string
	err := db.QueryRow(ctx, `SELECT envelope_jcs,envelope_sha FROM photo.read_v30_delivery_signed_envelope($1,$2,$3,$4)`,
		lease.Expansion, lease.Token, expected.ArtifactID, expected.SHA256).Scan(&raw, &sha)
	if errors.Is(err, pgx.ErrNoRows) { return invalid() }
	if err != nil { return delivery.SignedAssemblyEnvelope{}, nil, err }
	if len(raw) < 2 || len(raw) > 65536 || sha != expected.SHA256 || renderDerivativeBytesSHA([]byte(raw)) != sha { return invalid() }
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	var envelope delivery.SignedAssemblyEnvelope
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF ||
		envelope.SchemaVersion != 2 || envelope.Assembly.ArtifactID == uuid.Nil ||
		envelope.SigningClosure.ArtifactID == uuid.Nil ||
		envelope.Assembly.ArtifactID == envelope.SigningClosure.ArtifactID ||
		domainDigestInvalid(envelope.Assembly.SHA256) || domainDigestInvalid(envelope.SigningClosure.SHA256) ||
		expected.ArtifactID == envelope.Assembly.ArtifactID || expected.ArtifactID == envelope.SigningClosure.ArtifactID { return invalid() }
	exact, err := canonical.Marshal(envelope)
	if err != nil || !bytes.Equal(exact, []byte(raw)) { return invalid() }
	return envelope, append([]byte(nil), raw...), nil
}

