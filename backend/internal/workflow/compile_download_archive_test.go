package workflow

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestCompileDownloadArchivesStableAndOrdered(t *testing.T) {
	r, _ := LoadRegistry()
	run, snapshot := uuid.New(), uuid.New()
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	s := DownloadArchiveSpec{RunID: run, SnapshotID: snapshot, SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 2, Entries: []DownloadArchiveEntry{{Ordinal: 1, Artifact: ref, Bytes: 10, EntryName: "p.jpg", ProducerTask: uuid.New()}}}}}
	a, e := CompileDownloadArchives(s, r)
	b, _ := CompileDownloadArchives(s, r)
	if e != nil || len(a) != 1 || a[0].ID != b[0].ID || a[0].Key != "archive.build/0002" {
		t.Fatal(a, e)
	}
}
func TestCompileDownloadArchivesRejectsOversizeAndDuplicateNames(t *testing.T) {
	r, _ := LoadRegistry()
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	entry := DownloadArchiveEntry{Artifact: ref, Bytes: DefaultVolumeByteLimit, EntryName: "same.jpg", ProducerTask: uuid.New()}
	s := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{entry, entry}}}}
	if _, e := CompileDownloadArchives(s, r); e == nil {
		t.Fatal("invalid archive accepted")
	}
}
