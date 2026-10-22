package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestBackgroundCleanupPreviewReceiptMatchesExactCanonicalAttempt(t *testing.T) {
	var linkage BackgroundCleanupPreviewPresentationLinkage
	linkage.Document.TaskID = uuid.New()
	linkage.Document.ResultID = uuid.New()
	linkage.Document.Attempt = 2
	linkage.Document.Fence = 3
	digest := previewParamsFixture().Plan.SHA256
	newRef := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest} }
	linkage.Document.Source.Ref = newRef()
	linkage.Presentation.WorkingImage = newRef()
	linkage.Presentation.Pixels = newRef()
	linkage.Presentation.NumericalEvidence = newRef()
	receipt := BackgroundCleanupPreviewReceipt{SchemaVersion: 1, ParamsSHA: digest, TaskID: linkage.Document.TaskID,
		ResultID: linkage.Document.ResultID, Attempt: 2, Fence: 3, Presentation: linkage.Document.Source.Ref, WorkingImage: linkage.Presentation.WorkingImage, Pixels: linkage.Presentation.Pixels, NumericalEvidence: linkage.Presentation.NumericalEvidence}
	for _, change := range []string{"valid", "attempt", "fence", "result", "task", "presentation", "image", "pixels", "evidence", "alias", "params", "schema"} {
		value := receipt
		switch change {
		case "attempt":
			value.Attempt++
		case "fence":
			value.Fence++
		case "result":
			value.ResultID = uuid.New()
		case "task":
			value.TaskID = uuid.New()
		case "presentation":
			value.Presentation = newRef()
		case "image":
			value.WorkingImage = newRef()
		case "pixels":
			value.Pixels = newRef()
		case "evidence":
			value.NumericalEvidence = newRef()
		case "alias":
			value.Pixels = value.WorkingImage
		case "params":
			value.ParamsSHA = ""
		case "schema":
			value.SchemaVersion = 2
		}
		raw, err := canonical.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		_, err = decodeBackgroundCleanupPreviewReceipt(raw, linkage, digest)
		if (err == nil) != (change == "valid") {
			t.Fatal(change, err)
		}
	}
}

func TestBackgroundCleanupReceiptRejectsUnverifiedBeforeDatabaseIO(t *testing.T) {
	_, err := SealBackgroundCleanupPreviewReceipt(context.Background(), nil, auth.Identity{}, uuid.New(), PhysicalBackgroundCleanupPreview{})
	if err != ErrInvalid {
		t.Fatal(err)
	}
}

type cleanupCancelledReceiptQuery struct {
	cancel  context.CancelFunc
	scanErr error
	calls   int
}
type cleanupCancelledReceiptRow struct{ owner *cleanupCancelledReceiptQuery }

func (q *cleanupCancelledReceiptQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	q.calls++
	return cleanupCancelledReceiptRow{owner: q}
}
func (r cleanupCancelledReceiptRow) Scan(...any) error {
	r.owner.cancel()
	return r.owner.scanErr
}
func TestCleanupReceiptCancellationDuringSQLNeverReturnsReceipt(t *testing.T) {
	for _, scanErr := range []error{nil, errors.New("SQL failure after cancellation")} {
		ctx, cancel := context.WithCancel(context.Background())
		physical, _ := backgroundCleanupResponseFixture()
		run := uuid.New()
		physical.linkage.Document.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_background_cleanup:"+physical.linkage.Presentation.Params.TargetID.String()))
		query := &cleanupCancelledReceiptQuery{cancel: cancel, scanErr: scanErr}
		receipt, err := SealBackgroundCleanupPreviewReceipt(ctx, query,
			auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, run, physical)
		cancel()
		if !errors.Is(err, context.Canceled) || receipt != (BackgroundCleanupPreviewReceipt{}) || query.calls != 1 {
			t.Fatal("receipt escaped or cancellation lost", receipt, err, query.calls)
		}
	}
}
