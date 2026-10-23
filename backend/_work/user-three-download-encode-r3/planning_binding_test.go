package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/workflow"
	"reflect"
	"strings"
	"testing"
)

func TestActualBatchBindingBeforePublication(t *testing.T) {
	j := validJobs()[0]
	batch := workflow.DownloadExportBatch{JobID: j.Job, SnapshotID: j.Selection, Spec: workflow.DownloadExportSpec{RunID: j.Run}}
	hash, e := canonical.Hash(batch.Spec.Preset)
	if e != nil {
		t.Fatal(e)
	}
	j.PresetHash = hash
	if e = validateBatchBinding(batch, j); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*workflow.DownloadExportBatch){func(b *workflow.DownloadExportBatch) { b.JobID = uuid.New() }, func(b *workflow.DownloadExportBatch) { b.SnapshotID = uuid.New() }, func(b *workflow.DownloadExportBatch) { b.Spec.RunID = uuid.New() }, func(b *workflow.DownloadExportBatch) { q := 1; b.Spec.Preset.Quality = &q }} {
		bad := batch
		mutate(&bad)
		if validateBatchBinding(bad, j) == nil {
			t.Fatal("wrong job/selection/run/preset admitted")
		}
	}
}

type presetRows struct {
	pgx.Rows
	items  [][]any
	index  int
	closed bool
	err    error
}

func (r *presetRows) Next() bool { return r.index < len(r.items) }
func (r *presetRows) Scan(dst ...any) error {
	for i, v := range r.items[r.index] {
		reflect.ValueOf(dst[i]).Elem().Set(reflect.ValueOf(v))
	}
	r.index++
	return nil
}
func (r *presetRows) Close()     { r.closed = true }
func (r *presetRows) Err() error { return r.err }

type presetDB struct {
	rows *presetRows
	j    Job
}

func (d presetDB) Query(_ context.Context, q string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(q, "FROM photo.download_export_context($1,$2)") || len(args) != 2 || args[0] != d.j.Tenant || args[1] != d.j.Run {
		return nil, errors.New("not exact application-authorized context")
	}
	return d.rows, nil
}
func TestPersistedPresetIDUsesExistingContextAndClosesRows(t *testing.T) {
	j := validJobs()[0]
	for _, tc := range []struct {
		name  string
		items [][]any
		err   error
		bad   bool
	}{
		{"exact", [][]any{{j.Preset, j.PresetHash}}, nil, false}, {"wrongID", [][]any{{uuid.New(), j.PresetHash}}, nil, true}, {"wrongHash", [][]any{{j.Preset, "bad"}}, nil, true}, {"absent", nil, nil, true}, {"duplicate", [][]any{{j.Preset, j.PresetHash}, {j.Preset, j.PresetHash}}, nil, true}, {"queryCompletionError", nil, errors.New("typed DB error"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := &presetRows{items: tc.items, err: tc.err}
			e := validatePersistedPreset(context.Background(), presetDB{rows, j}, j)
			if (e != nil) != tc.bad || !rows.closed {
				t.Fatal("bad preset binding or leaked open rows", e)
			}
		})
	}
}

func TestPublicationNotInvokedForBadBatchOrSources(t *testing.T) {
	j := validJobs()[0]
	batch := workflow.DownloadExportBatch{JobID: j.Job, SnapshotID: j.Selection, Spec: workflow.DownloadExportSpec{RunID: j.Run}}
	j.PresetHash, _ = canonical.Hash(batch.Spec.Preset)
	var sources []SourceBinding
	for i := 0; i < 3; i++ {
		x := SourceBinding{Master: uuid.New(), MasterSHA: strings.Repeat("a", 64), Pixels: uuid.New(), PixelsSHA: strings.Repeat("b", 64), Width: 4180, Height: 6264}
		sources = append(sources, x)
		batch.Spec.Items = append(batch.Spec.Items, workflow.DownloadExportItem{Ordinal: int64(i), Manifest: review.ArtifactRef{ID: x.Master, SHA256: x.MasterSHA}, Image: review.WorkingImage{Pixels: review.ArtifactRef{ID: x.Pixels, SHA256: x.PixelsSHA}, Canvas: review.Canvas{Width: x.Width, Height: x.Height}}})
	}
	calls := 0
	publish := func() (bool, error) { calls++; return true, nil }
	for _, mutate := range []func(*workflow.DownloadExportBatch){func(b *workflow.DownloadExportBatch) { b.JobID = uuid.New() }, func(b *workflow.DownloadExportBatch) { b.SnapshotID = uuid.New() }, func(b *workflow.DownloadExportBatch) { b.Spec.RunID = uuid.New() }, func(b *workflow.DownloadExportBatch) { b.Spec.Preset.Format = "wrong" }} {
		bad := batch
		mutate(&bad)
		if _, e := approvedBatchThenPublish(bad, j, sources, publish); e == nil || calls != 0 {
			t.Fatal("publication invoked before exact admission")
		}
	}
	badSources := append([]SourceBinding(nil), sources...)
	badSources[0].Width = 2560
	if _, e := approvedBatchThenPublish(batch, j, badSources, publish); e == nil || calls != 0 {
		t.Fatal("publication invoked for wrong source dimensions")
	}
	if ok, e := approvedBatchThenPublish(batch, j, sources, publish); e != nil || !ok || calls != 1 {
		t.Fatal("valid batch not published once")
	}
}
