package v2optical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveLinesScope binds line evidence to the exact accepted source and
// principal. A catalogue must recheck this authority on every Resolve call.
type PerspectiveLinesScope struct {
	Tenant, Project, Snapshot uuid.UUID
	Actor, Session            uuid.UUID
	Base                      v2input.SessionVersionBinding
	Lines                     v2input.Ref
}
type PerspectiveLinesLocator struct {
	Scope                       PerspectiveLinesScope
	Bucket, Key, Version, Media string
	Bytes                       int64
}
type PerspectiveLinesCatalog interface {
	ResolvePerspectiveLines(context.Context, PerspectiveLinesScope) (PerspectiveLinesLocator, error)
}
type PerspectiveLinesLoader struct {
	Catalog PerspectiveLinesCatalog
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

// Load performs bounded original-byte reading outside catalogue transactions,
// verifies exact canonical bytes and repeats the scope/version resolution.
// It never accepts line bytes or object coordinates supplied by a public caller.
func (s PerspectiveLinesLoader) Load(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, params PerspectiveParams) (PerspectiveLineSegments, error) {
	var zero PerspectiveLineSegments
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Catalog == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	if params.Mode != "auto" || params.Lines == nil || params.Lines.ArtifactID == params.Base.Source.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	scope := PerspectiveLinesScope{Tenant: tenant, Project: project, Snapshot: snapshot, Actor: who.UserID, Session: who.SessionID, Base: params.Base, Lines: *params.Lines}
	locator, err := s.Catalog.ResolvePerspectiveLines(ctx, scope)
	if err != nil {
		return zero, err
	}
	if locator.Scope != scope || locator.Bucket != s.Bucket || locator.Key == "" || len(locator.Key) > 1024 || locator.Version == "" || locator.Version == "null" || len(locator.Version) > 1024 || locator.Media != "application/json" || locator.Bytes < 2 || locator.Bytes > 1048576 {
		return zero, v2input.ErrInvalid
	}
	reader, err := s.Objects.Open(ctx, objectstore.Object{Key: locator.Key, VersionID: locator.Version, SHA256: scope.Lines.SHA256, Bytes: locator.Bytes, ContentType: locator.Media})
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return zero, err
	}
	if reader == nil {
		return zero, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, locator.Bytes+1))
	closeErr := reader.Close()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	hash := sha256.Sum256(raw)
	if int64(len(raw)) != locator.Bytes || hex.EncodeToString(hash[:]) != scope.Lines.SHA256 {
		return zero, v2input.ErrInvalid
	}
	lines, err := ParsePerspectiveLineSegments(raw)
	if err != nil {
		return zero, err
	}
	current, err := s.Catalog.ResolvePerspectiveLines(ctx, scope)
	if err != nil {
		return zero, err
	}
	if current != locator {
		return zero, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return lines, nil
}
