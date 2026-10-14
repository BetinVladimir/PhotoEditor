package v2draft

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type restorationOriginRow struct {
	role           string
	root, original v2input.Ref
}

func (r restorationOriginRow) Scan(dest ...any) error {
	if len(dest) == 1 {
		*dest[0].(*string) = r.role
		return nil
	}
	if len(dest) != 4 {
		return errors.New("unexpected origin columns")
	}
	*dest[0].(*uuid.UUID) = r.root.ArtifactID
	*dest[1].(*string) = r.root.SHA256
	*dest[2].(*uuid.UUID) = r.original.ArtifactID
	*dest[3].(*string) = r.original.SHA256
	return nil
}

type restorationOriginTx struct {
	pgx.Tx
	role           string
	root, original v2input.Ref
	queries        []string
	args           []any
	tenant         string
}

func (t *restorationOriginTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	t.queries = append(t.queries, query)
	if len(args) > 0 {
		t.args = args
	}
	return restorationOriginRow{role: t.role, root: t.root, original: t.original}
}
func (t *restorationOriginTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	t.tenant = args[0].(string)
	return pgconn.CommandTag{}, nil
}

func TestRestorationManualScopesUseOriginalSQLProducer(t *testing.T) {
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(fixture, &images) != nil || len(images) == 0 {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
	canvasSHA, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(sourceRaw)}, CanvasHash: canvasSHA}
	target := review.MaskRef{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "manual", Confidence: 1}
	guard := target
	guard.Artifact.ID = uuid.New()
	guard.Protected = true
	targetRaw, err := canonical.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	guardRaw, err := canonical.Marshal(guard)
	if err != nil {
		t.Fatal(err)
	}
	targetRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(targetRaw)}
	guardRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(guardRaw)}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	tx := &restorationOriginTx{role: "photo_scheduler", root: root, original: original}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
	scopes, err := LockRestorationManualScopes(context.Background(), tx, who, tenant, project, snapshot,
		base, sourceRaw, targetRaw, guardRaw, targetRef, guardRef)
	if err != nil || scopes.RootRef() != root || scopes.OriginalParamsRef() != original ||
		scopes.target.ParentRef != (v2input.Ref{}) || scopes.protected.ParentRef != (v2input.Ref{}) ||
		tx.tenant != tenant.String() || len(tx.queries) != 2 || len(tx.args) != 14 ||
		!strings.Contains(tx.queries[1], "photo.v28_manual_pair_origin") {
		t.Fatal("original mask origin binding changed", err)
	}
	guardRaw[0] = ' '
	tx.queries = nil
	if _, err := LockRestorationManualScopes(context.Background(), tx, who, tenant, project, snapshot,
		base, sourceRaw, targetRaw, guardRaw, targetRef, guardRef); err == nil || len(tx.queries) != 0 {
		t.Fatal("changed mask bytes reached SQL")
	}
}
