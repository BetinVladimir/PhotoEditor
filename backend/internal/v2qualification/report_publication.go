// Versioned qualification-report publication with independently verified locks.
// Installation requires the maintenance-only SQL protocol attested at startup.
package v2qualification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

type ReportPublicationScope struct {
	Tenant, Project, Operator uuid.UUID
	Key                       string
}
type ReportPublicationReservation struct {
	Artifact, Token uuid.UUID
	WriterState     string
	Pinned          *objectstore.Object
	Ready           *objectstore.Object
}

// New maintenance-only repository must commit Reserve before IO; bind exact
// scope/content/Expected policy, quota, token and artifact identity durably.
// Check and Complete repeat CURRENT operator/project/policy/reservation checks.
// Complete records only the authenticated maintenance writer's verified tuple;
// it never creates model_qualifications or enables any policy/operation.
// Unknown completion/IO leaves pending accounting for an OWNED mounted cleanup
// or exact replay; it must not refund without physical cleanup reconciliation.
type ReportPublicationRepository interface {
	Reserve(context.Context, ReportPublicationScope, Expected, string, int64) (ReportPublicationReservation, error)
	Check(context.Context, ReportPublicationScope, Expected, ReportPublicationReservation, string, int64) error
	Start(context.Context, ReportPublicationScope, ReportPublicationReservation) error
	Close(context.Context, ReportPublicationScope, ReportPublicationReservation, objectstore.Object) error
	Complete(context.Context, ReportPublicationScope, Expected, ReportPublicationReservation, objectstore.Object) (artifactjson.Ref, error)
}
type ReportPublicationObjects interface {
	artifactjson.Objects
	ObjectReader
}
type ReportPublisher struct {
	Repository ReportPublicationRepository
	Objects    ReportPublicationObjects
}

func reportPublicationKey(s ReportPublicationScope, id uuid.UUID) string {
	return "tenants/" + s.Tenant.String() + "/qualification-reports/projects/" + s.Project.String() + "/reports/" + id.String() + ".json"
}
func validPublishedReport(o objectstore.Object, key, sha string, size int64) bool {
	return o.Key == key && o.SHA256 == sha && o.Bytes == size && o.ContentType == "application/json" && o.VersionID != "" && o.VersionID != "null"
}

// Leaf and parent methods use distinct protected policy types. The internal
// transport does not accept a verifier callback or caller-supplied admitted flag.
func (p ReportPublisher) Publish(ctx context.Context, s ReportPublicationScope, raw []byte, expected Expected) (artifactjson.Ref, error) {
	return p.publishTyped(ctx, s, raw, &expected, nil)
}
func (p ReportPublisher) PublishMixed(ctx context.Context, s ReportPublicationScope, raw []byte, expected MixedExpected) (artifactjson.Ref, error) {
	return p.publishTyped(ctx, s, raw, nil, &expected)
}
func (p ReportPublisher) publishTyped(ctx context.Context, s ReportPublicationScope, raw []byte, leaf *Expected, parent *MixedExpected) (artifactjson.Ref, error) {
	if (leaf == nil) == (parent == nil) {
		return artifactjson.Ref{}, ErrUnqualified
	}
	var expected Expected
	if leaf != nil {
		expected = *leaf
	} else {
		expected = parent.Common
	}

	zero := artifactjson.Ref{}
	if p.Repository == nil || p.Objects == nil || s.Tenant == uuid.Nil || s.Project == uuid.Nil || s.Operator == uuid.Nil || len(s.Key) < 1 || len(s.Key) > 200 || len(raw) < 2 || len(raw) > 8*1024*1024 {
		return zero, ErrUnqualified
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	body := append([]byte(nil), raw...)
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	size := int64(len(body))
	// Verify actual measured bytes against independently loaded deployment locks.
	// No canonical rewrite, status promotion, synthesized evidence or skip flag.
	if leaf != nil {
		if err := VerifyImportReport(body, sha, *leaf); err != nil {
			return zero, err
		}
	} else {
		if _, err := VerifyMixed(body, sha, *parent); err != nil {
			return zero, err
		}
	}
	reservation, err := p.Repository.Reserve(ctx, s, expected, sha, size)
	if err != nil {
		return zero, err
	}
	if reservation.Artifact == uuid.Nil || reservation.Token == uuid.Nil {
		return zero, objectstore.ErrIntegrity
	}
	key := reportPublicationKey(s, reservation.Artifact)
	if err = p.Repository.Check(ctx, s, expected, reservation, sha, size); err != nil {
		return zero, err
	}
	var object objectstore.Object
	if reservation.Ready != nil {
		object = *reservation.Ready // Original pinned version; never latest fallback.
	} else {
		if reservation.WriterState == "idle" {
			if err = p.Repository.Start(ctx, s, reservation); err != nil {
				return zero, err
			}
			object, err = p.Objects.PutImmutable(ctx, key, bytes.NewReader(body), size, sha, "application/json")
		} else if reservation.WriterState == "open" {
			object, err = p.Objects.RecoverImmutable(ctx, key, size, sha, "application/json")
		} else if reservation.WriterState == "closed" && reservation.Pinned != nil {
			object = *reservation.Pinned
		} else {
			return zero, objectstore.ErrIntegrity
		}
		if errors.Is(err, objectstore.ErrExists) || errors.Is(err, objectstore.ErrUnavailable) {
			if ctx.Err() != nil {
				return zero, ctx.Err()
			}
			object, err = p.Objects.RecoverImmutable(ctx, key, size, sha, "application/json")
		}
		if err != nil {
			return zero, err
		}
	}
	if !validPublishedReport(object, key, sha, size) {
		return zero, objectstore.ErrIntegrity
	}
	if err = p.Repository.Check(ctx, s, expected, reservation, sha, size); err != nil {
		return zero, err
	}
	reader, err := p.Objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return zero, err
	}
	if reader == nil {
		return zero, objectstore.ErrIntegrity
	}
	observed, readErr := io.ReadAll(io.LimitReader(reader, 8*1024*1024+1))
	closeErr := reader.Close()
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if int64(len(observed)) != size || !bytes.Equal(observed, body) {
		return zero, objectstore.ErrIntegrity
	}
	if err = p.Repository.Check(ctx, s, expected, reservation, sha, size); err != nil {
		return zero, err
	}
	wanted := artifactjson.Ref{ID: reservation.Artifact, SHA256: sha}
	if reservation.Ready != nil {
		return wanted, nil
	}
	if err = p.Repository.Close(ctx, s, reservation, object); err != nil {
		return zero, err
	}
	published, err := p.Repository.Complete(ctx, s, expected, reservation, object)
	if err != nil {
		return zero, err
	}
	if published != wanted {
		return zero, objectstore.ErrIntegrity
	}
	return published, nil
}
