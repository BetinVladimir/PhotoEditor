package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

type archivePublicationRow struct{}

func (archivePublicationRow) Scan(values ...any) error { *(values[0].(*bool)) = true; return nil }

type archivePublicationDB struct{}

func (archivePublicationDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return archivePublicationRow{}
}

func TestArchiveMaterializeRejectsOversizedInputsBeforeSQL(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]DownloadArchiveEntry, MaxDownloadArchiveInputs)
	for i := range entries {
		entries[i] = DownloadArchiveEntry{Ordinal: int64(i), Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, EntryName: fmt.Sprintf("%04d.jpg", i), ProducerTask: uuid.New()}
	}
	run := uuid.New()
	nodes, err := CompileDownloadArchives(DownloadArchiveSpec{RunID: run, SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 1, Entries: entries}}}, registry)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(nodes[0].Params)
	valid, err := PrepareDownloadArchiveTask(PublishedDownloadArchive{Node: nodes[0], Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}})
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{5000, 5001} {
		task := valid
		task.Inputs = append([]review.ArtifactRef(nil), valid.Inputs...)
		if count == 5001 {
			task.Inputs = append(task.Inputs, review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("c", 64)})
		}
		task.InputDigest, err = repairInputDigest(task.Operation, task.Version, task.Params, task.Inputs)
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		repository := DownloadArchiveTaskRepository{DB: archiveRegistrationCallbackDB{callback: func(context.Context, string, ...any) pgx.Row { calls++; return archivePublicationRow{} }}}
		ok, err := repository.Materialize(context.Background(), uuid.New(), run, task)
		if count == 5000 {
			if !ok || err != nil || calls != 1 {
				t.Fatal("valid boundary refused", ok, err, calls)
			}
		} else if ok || err == nil || calls != 0 {
			t.Fatal("oversized inventory reached SQL", ok, err, calls)
		}
	}
}

type archivePublicationCallback func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error)

func (f archivePublicationCallback) Publish(ctx context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	return f(ctx, scope, body)
}

func TestArchivePublicationOwnsBytesAndBindsOriginalDigest(t *testing.T) {
	for _, outcome := range []string{"original_digest", "mutated_digest", "zero_identity", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64),
				Volumes: []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{{Ordinal: 0,
					Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1,
					EntryName: "frame.jpg", ProducerTask: uuid.New()}}}}}
			registry, err := LoadRegistry()
			if err != nil {
				t.Fatal(err)
			}
			compiled, err := CompileDownloadArchives(spec, registry)
			if err != nil {
				t.Fatal(err)
			}
			original := append([]byte(nil), compiled[0].Params...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			publisher := archivePublicationCallback(func(_ context.Context, scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
				originalHash := sha256.Sum256(body)
				body[0] = 'x'
				ref := artifactjson.Ref{ID: uuid.New(), SHA256: hex.EncodeToString(originalHash[:])}
				if outcome == "mutated_digest" {
					changed := sha256.Sum256(body)
					ref.SHA256 = hex.EncodeToString(changed[:])
				}
				if outcome == "zero_identity" {
					ref.ID = uuid.Nil
				}
				if outcome == "cancelled" {
					cancel()
				}
				return ref, nil
			})
			builder := DownloadArchiveBuilder{Plans: DownloadArchiveRepository{DB: archivePublicationDB{}}, Publisher: publisher}
			nodes, err := builder.Publish(ctx, uuid.New(), spec)
			if outcome == "original_digest" {
				if err != nil || len(nodes) != 1 || !bytes.Equal(nodes[0].Node.Params, original) {
					t.Fatal("publisher mutated frozen params", err)
				}
			} else {
				if err == nil || nodes != nil {
					t.Fatal("invalid publication escaped", nodes)
				}
				if outcome == "cancelled" && err != context.Canceled {
					t.Fatal("cancellation lost", err)
				}
			}
		})
	}
}

type archiveRegistrationCallbackRow struct{ callback func(...any) error }

func (r archiveRegistrationCallbackRow) Scan(values ...any) error { return r.callback(values...) }

type archiveRegistrationCallbackDB struct {
	callback func(context.Context, string, ...any) pgx.Row
}

func (d archiveRegistrationCallbackDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return d.callback(ctx, query, args...)
}

func TestArchiveRegistrationCancellationStopsBeforeFollowingVolumeOrPublication(t *testing.T) {
	for _, outcome := range []string{"accepted", "refused", "sql_error"} {
		t.Run(outcome, func(t *testing.T) {
			spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64)}
			for index := 0; index < 2; index++ {
				spec.Volumes = append(spec.Volumes, DownloadArchiveVolume{Number: index + 1, Entries: []DownloadArchiveEntry{{
					Ordinal: int64(index), Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
					Bytes: 1, EntryName: "frame.jpg", ProducerTask: uuid.New()}}})
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			primary := errors.New("SQL scan failed")
			registrations, publications := 0, 0
			db := archiveRegistrationCallbackDB{callback: func(context.Context, string, ...any) pgx.Row {
				registrations++
				return archiveRegistrationCallbackRow{callback: func(values ...any) error {
					cancel()
					if outcome == "sql_error" {
						return primary
					}
					*(values[0].(*bool)) = outcome == "accepted"
					return nil
				}}
			}}
			publisher := archivePublicationCallback(func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
				publications++
				return artifactjson.Ref{}, nil
			})
			nodes, err := (DownloadArchiveBuilder{Plans: DownloadArchiveRepository{DB: db}, Publisher: publisher}).Publish(ctx, uuid.New(), spec)
			expected := error(context.Canceled)
			if outcome == "sql_error" {
				expected = primary
			}
			if err != expected || nodes != nil || registrations != 1 || publications != 0 {
				t.Fatal("registration continued or lost primary error", err, registrations, publications)
			}
		})
	}
}

func TestArchiveTaskFinalizationCancellationAndSQLErrorPrecedence(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	spec := DownloadArchiveSpec{RunID: run, SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64),
		Volumes: []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{{Ordinal: 0,
			Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
			Bytes:    1, EntryName: "frame.jpg", ProducerTask: uuid.New()}}}}}
	nodes, err := CompileDownloadArchives(spec, registry)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(nodes[0].Params)
	task, err := PrepareDownloadArchiveTask(PublishedDownloadArchive{Node: nodes[0],
		Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"materialize", "seal"} {
		for _, outcome := range []string{"accepted", "refused", "sql_error", "already_cancelled"} {
			t.Run(phase+"_"+outcome, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				primary := errors.New("SQL finalization failure")
				calls := 0
				db := archiveRegistrationCallbackDB{callback: func(context.Context, string, ...any) pgx.Row {
					calls++
					return archiveRegistrationCallbackRow{callback: func(values ...any) error {
						cancel()
						if outcome == "sql_error" {
							return primary
						}
						*(values[0].(*bool)) = outcome == "accepted"
						return nil
					}}
				}}
				if outcome == "already_cancelled" {
					cancel()
				}
				repository := DownloadArchiveTaskRepository{DB: db}
				var ok bool
				var err error
				if phase == "materialize" {
					ok, err = repository.Materialize(ctx, uuid.New(), run, task)
				} else {
					ok, err = repository.Seal(ctx, uuid.New(), run)
				}
				expected := error(context.Canceled)
				if outcome == "sql_error" {
					expected = primary
				}
				expectedCalls := 1
				if outcome == "already_cancelled" {
					expectedCalls = 0
				}
				if ok || err != expected || calls != expectedCalls {
					t.Fatal("cancelled finalization reported success", ok, err, calls)
				}
			})
		}
	}
}
