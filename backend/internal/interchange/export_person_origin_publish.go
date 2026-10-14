package interchange

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// The repository must reserve a deterministic entry-scoped receipt under the
// current tenant/project/source authority and expose it as a declared export
// task input only after Complete proves the exact immutable object version.
type ExportPersonOriginRepository interface {
	ReservePersonOrigin(context.Context, PortablePersonOriginExpectation, PreparedPersonOriginAttachment) (artifactjson.Reservation, error)
	CompletePersonOrigin(context.Context, PortablePersonOriginExpectation, PreparedPersonOriginAttachment,
		artifactjson.Reservation, objectstore.Object) (v2input.Ref, error)
}

type ExportPersonOriginAttachmentPublisher struct {
	Tenant, Run uuid.UUID
	Repository  ExportPersonOriginRepository
	Objects     artifactjson.Objects
	Reader      ImportDocumentObjects
	Current     ExportPersonOriginCurrentChecker
	Trusted     map[PortablePersonOriginKey]ed25519.PublicKey
}

func (p ExportPersonOriginAttachmentPublisher) Publish(ctx context.Context,
	scope PortablePersonOriginExpectation, prepared PreparedPersonOriginAttachment) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Tenant == uuid.Nil || p.Run == uuid.Nil || p.Repository == nil || p.Objects == nil ||
		p.Reader == nil || p.Current == nil || len(p.Trusted) == 0 ||
		!validImportRef(prepared.Ref) || prepared.Ref.SHA256 != rawSHA(prepared.Body) ||
		len(prepared.Body) < 2 || len(prepared.Body) > 16384 ||
		prepared.Mapping.EntryID != scope.EntryID || prepared.Mapping.Recipe != scope.Recipe ||
		prepared.Mapping.Override != scope.Override || prepared.Mapping.MaskRef != scope.MaskRef ||
		prepared.Mapping.MaskPixels != scope.MaskPixels || prepared.Mapping.Path != prepared.Path ||
		prepared.Mapping.SHA != prepared.Ref.SHA256 ||
		prepared.Path != "origins/"+scope.EntryID.String()+"/"+scope.MaskRef.ArtifactID.String()+".json" ||
		prepared.Ref.ArtifactID != uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())) {
		return zero, v2input.ErrInvalid
	}
	body := prepared.OwnedBody()
	claim, err := VerifyPortablePersonOrigin(body, scope, p.Trusted)
	if err != nil {
		return zero, err
	}
	if err := p.Current.RecheckExportPersonOrigin(ctx, claim); err != nil {
		return zero, err
	}
	owned := prepared
	owned.Body = body
	reservation, err := p.Repository.ReservePersonOrigin(ctx, scope, owned)
	if err != nil {
		return zero, err
	}
	if reservation.ArtifactID != prepared.Ref.ArtifactID {
		return zero, objectstore.ErrIntegrity
	}
	key, err := objectstore.DocumentKey(p.Tenant, p.Run, prepared.Ref.ArtifactID)
	if err != nil {
		return zero, err
	}
	var object objectstore.Object
	if reservation.Ready == nil {
		if reservation.Token == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		object, err = p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), int64(len(body)), prepared.Ref.SHA256, "application/json")
		if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			object, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), prepared.Ref.SHA256, "application/json")
		}
		if err != nil {
			return zero, err
		}
	} else {
		if reservation.Ready.ID != prepared.Ref.ArtifactID || reservation.Ready.SHA256 != prepared.Ref.SHA256 {
			return zero, objectstore.ErrIntegrity
		}
		object, err = p.Objects.RecoverImmutable(ctx, key, int64(len(body)), prepared.Ref.SHA256, "application/json")
		if err != nil {
			return zero, err
		}
	}
	if object.Key != key || object.SHA256 != prepared.Ref.SHA256 || object.Bytes != int64(len(body)) ||
		object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return zero, objectstore.ErrIntegrity
	}
	stream, err := p.Reader.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return zero, err
	}
	if stream == nil {
		return zero, v2input.ErrInvalid
	}
	stored, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, int64(len(body))+1))
	closeErr := stream.Close()
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	if !bytes.Equal(stored, body) {
		return zero, objectstore.ErrIntegrity
	}
	if err := p.Current.RecheckExportPersonOrigin(ctx, claim); err != nil {
		return zero, err
	}
	if reservation.Ready != nil {
		return prepared.Ref, ctx.Err()
	}
	actual, err := p.Repository.CompletePersonOrigin(ctx, scope, owned, reservation, object)
	if err != nil {
		return zero, err
	}
	if actual != prepared.Ref {
		return zero, objectstore.ErrIntegrity
	}
	return actual, ctx.Err()
}
