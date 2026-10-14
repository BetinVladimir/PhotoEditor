package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

func TestSQLImportPersonAcceptanceBindsOnlyCurrentWorkflow(t *testing.T) {
	var binder SQLImportPersonAcceptanceBinder
	if err := binder.QualifyImportedMask(context.Background(), review.MaskRef{}, v2input.SessionVersionBinding{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unbound acceptance admitted a mask")
	}
	raw, err := canonical.Marshal(importParsedValidatorFixture())
	if err != nil {
		t.Fatal(err)
	}
	c := workflow.InterchangeImportCandidate{
		Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Project: uuid.New(),
		Artifact: uuid.New(), SHA: rawSHA(raw), Bucket: "photos",
	}
	deps := ImportResolverDependencies{DB: &pgxpool.Pool{}, Bucket: c.Bucket}
	if q, err := binder.BindImportPersonAcceptance(context.Background(), c, raw, ImportResolverDependencies{}); !errors.Is(err, v2input.ErrInvalid) || q != nil {
		t.Fatal("missing scheduler database accepted")
	}
	wrong := c
	wrong.SHA = rawSHA([]byte("foreign parsed document"))
	if q, err := binder.BindImportPersonAcceptance(context.Background(), wrong, raw, deps); !errors.Is(err, v2input.ErrInvalid) || q != nil {
		t.Fatal("foreign parsed document accepted")
	}
	q, err := binder.BindImportPersonAcceptance(context.Background(), c, raw, deps)
	if err != nil {
		t.Fatal(err)
	}
	bound, ok := q.(SQLImportConfirmedMaskAuthority)
	if !ok || bound.Store.Tenant != c.Tenant || bound.Store.Identity.Run != c.Run ||
		bound.Store.Identity.Workflow != c.Workflow || bound.Store.Bucket != c.Bucket ||
		string(bound.Store.Parsed) != string(raw) {
		t.Fatal("acceptance lost trusted workflow scope")
	}
	raw[0] ^= 1
	if string(bound.Store.Parsed) == string(raw) {
		t.Fatal("bound acceptance retained mutable parsed bytes")
	}
}
