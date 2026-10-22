package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestCatalogRegistrationRevalidatesManifestBeforeSQL(t *testing.T) {
	img, outputs, metadata, icc := decodedFixture(t)
	raw, _ := canonical.Marshal(img)
	outputs.Manifest = metadataRef(raw)
	sourceMeta, _ := canonical.Marshal(inspectedAsset{Source: metadata.Source, Width: 400, Height: 300, BitDepth: 8, Orientation: 1, Format: "jpeg"})
	input := CatalogImage{Ordinal: 9, Source: metadata.Source, DecodeTaskID: uuid.New(), MetadataRef: metadataRef(sourceMeta), Metadata: metadata, Outputs: outputs, Image: img}
	db := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := IngestCatalogRepository{DB: db, WorkICCSHA: icc}
	tenant, run := uuid.New(), uuid.New()
	ok, err := repo.AdoptImage(context.Background(), tenant, run, input)
	if err != nil || !ok || db.sql != "SELECT photo.adopt_ingest_image($1,$2,$3,$4,$5,$6,$7)" || len(db.args) != 7 || db.args[0] != tenant || db.args[1] != run || db.args[2] != int64(9) || db.args[3] != outputs.Manifest.ID || db.args[4] != outputs.Manifest.SHA256 {
		t.Fatal("catalog registration binding lost", err)
	}
	db.args = nil
	input.Image.Canvas.Width++
	if ok, err = repo.AdoptImage(context.Background(), tenant, run, input); err == nil || ok || db.args != nil {
		t.Fatal("mutated manifest reached SQL")
	}
}

func TestCatalogRejectFileBindsSourceAndTerminalReason(t *testing.T) {
	spec, _ := ingestFixture(t)
	item := spec.Items[0]
	f := IngestItemFailure{item.Ordinal, item.Source, "asset.decode", "failed"}
	db := &expansionQueries{row: expansionRow{values: []any{true}}}
	repo := IngestCatalogRepository{DB: db}
	tenant := uuid.New()
	ok, err := repo.RejectFile(context.Background(), tenant, spec.RunID, f)
	if err != nil || !ok || len(db.args) != 7 || db.args[3] != item.Source.ID || db.args[4] != item.Source.SHA256 || db.args[5] != f.Stage || db.args[6] != f.Reason {
		t.Fatal("rejection scope lost", err)
	}
	for _, reason := range []string{"retry_wait", "running", "succeeded", ""} {
		db.args = nil
		f.Reason = reason
		if _, err = repo.RejectFile(context.Background(), tenant, spec.RunID, f); err == nil || db.args != nil {
			t.Fatal("nonterminal failure reached SQL")
		}
	}
}

func TestIngestFinishPreservesPendingAndTerminalStatuses(t *testing.T) {
	for _, state := range []string{"", "completed", "completed_with_errors", "failed", "running", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			var value *string
			if state != "" {
				value = &state
			}
			db := &expansionQueries{row: expansionRow{values: []any{value}}}
			repo := IngestCatalogRepository{DB: db}
			tenant, run := uuid.New(), uuid.New()
			got, err := repo.Finish(context.Background(), tenant, run)
			invalid := state == "running" || state == "cancelled"
			if invalid {
				if err == nil {
					t.Fatal("invalid terminal accepted")
				}
				return
			}
			if err != nil || got != state || len(db.args) != 2 || db.args[0] != tenant || db.args[1] != run {
				t.Fatal("completion scope/status lost", err)
			}
		})
	}
}

func TestCatalogRefusalChecksArchivedDestinationWithoutClaimingAdoption(t *testing.T) {
	img, outputs, metadata, icc := decodedFixture(t)
 raw,_:=canonical.Marshal(img);outputs.Manifest=metadataRef(raw)
	input := CatalogImage{Ordinal: 0, Source: metadata.Source, DecodeTaskID: uuid.New(), MetadataRef: metadataRef([]byte("{}")), Metadata: metadata, Outputs: outputs, Image: img}
	db := &archiveAdoptionQueries{}
	repo := IngestCatalogRepository{DB: db, WorkICCSHA: icc}
	tenant, run := uuid.New(), uuid.New()
	ok, err := repo.AdoptImage(context.Background(), tenant, run, input)
	if err != nil || ok || len(db.calls) != 2 || db.calls[1] != "SELECT photo.request_archived_ingest_cancel($1,$2)" {
		t.Fatal("archive refusal lifecycle lost", ok, err, db.calls)
	}
	if len(db.args[1]) != 2 || db.args[1][0] != tenant || db.args[1][1] != run {
		t.Fatal("archive cancellation scope changed")
	}
}

type archiveAdoptionQueries struct {
	calls []string
	args  [][]any
}

func (q *archiveAdoptionQueries) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls = append(q.calls, sql)
	q.args = append(q.args, args)
	return expansionRow{values: []any{len(q.calls) == 2}}
}
