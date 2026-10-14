package v2learning

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type seriesRejectRepository struct{ calls int }

func (r *seriesRejectRepository) Reserve(context.Context,
	artifactjson.Scope, string, int64) (artifactjson.Reservation, error) {
	r.calls++
	return artifactjson.Reservation{}, errors.New("SQL_REFUSED")
}
func (r *seriesRejectRepository) Complete(context.Context,
	artifactjson.Scope, artifactjson.Reservation,
	objectstore.Object) (artifactjson.Ref, error) {
	r.calls++
	return artifactjson.Ref{}, errors.New("UNEXPECTED_COMPLETE")
}

type seriesNoObjects struct{}

func (seriesNoObjects) PutImmutable(context.Context, string, io.Reader,
	int64, string, string) (objectstore.Object, error) {
	return objectstore.Object{}, errors.New("UNEXPECTED_PUT")
}
func (seriesNoObjects) RecoverImmutable(context.Context, string,
	int64, string, string) (objectstore.Object, error) {
	return objectstore.Object{}, errors.New("UNEXPECTED_RECOVERY")
}

func TestSeriesPublicationChecksManifestBeforeSQLReservation(t *testing.T) {
	document := seriesHoldoutFixture()
	body, sha, err := CompileFrozenSeriesHoldoutEvidence(document)
	if err != nil {
		t.Fatal(err)
	}
	draft := TrainingDraftPrepared{TenantID: document.TenantID,
		RunID: uuid.New(), ProjectID: uuid.New(), ActorID: uuid.New(),
		AuthSessionID: uuid.New()}
	ref, err := SeriesHoldoutRef(draft.RunID, document.EventID, sha)
	if err != nil {
		t.Fatal(err)
	}
	draft.Manifest.Examples = []ManifestExample{{EventID: document.EventID,
		RenderCase: TrainingRenderCase{SeriesEvidence: &ref,
			Binding: v2input.SessionVersionBinding{
				AssetVersionID: document.AssetVersionID,
				Source:         document.SourceImage},
			WorkingImage: document.SourceImage}}}
	prepared := PreparedSeriesHoldout{Document: document, Body: body, SHA256: sha}
	repository := &seriesRejectRepository{}
	publisher := artifactjson.Publisher{Repository: repository, Objects: seriesNoObjects{}}
	if _, err := PublishSeriesHoldout(context.Background(), publisher,
		draft, prepared); err == nil || repository.calls != 1 {
		t.Fatal("exact series document did not reach SQL authority")
	}
	for _, change := range []func(*TrainingDraftPrepared, *PreparedSeriesHoldout){
		func(d *TrainingDraftPrepared, _ *PreparedSeriesHoldout) {
			d.Manifest.Examples[0].RenderCase.SeriesEvidence = nil
		},
		func(d *TrainingDraftPrepared, _ *PreparedSeriesHoldout) {
			d.Manifest.Examples[0].RenderCase.WorkingImage.ArtifactID = uuid.New()
		},
		func(_ *TrainingDraftPrepared, p *PreparedSeriesHoldout) {
			p.Document.ItemID = uuid.New()
		},
		func(_ *TrainingDraftPrepared, p *PreparedSeriesHoldout) {
			p.Body = append(append([]byte(nil), p.Body...), ' ')
		},
	} {
		changedDraft := draft
		changedDraft.Manifest.Examples = append([]ManifestExample(nil), draft.Manifest.Examples...)
		changed := prepared
		change(&changedDraft, &changed)
		before := repository.calls
		if _, err := PublishSeriesHoldout(context.Background(), publisher,
			changedDraft, changed); err == nil || repository.calls != before {
			t.Fatal("substituted series document reached SQL reservation")
		}
	}
}
