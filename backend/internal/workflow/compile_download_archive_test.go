package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

func TestArchiveCompilerTransportBoundary(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{5000, 5001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			entries := make([]DownloadArchiveEntry, count)
			for i := range entries {
				entries[i] = DownloadArchiveEntry{Ordinal: int64(i), Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, EntryName: fmt.Sprintf("%04d.jpg", i), ProducerTask: uuid.New()}
			}
			spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 1, Entries: entries}}}
			nodes, err := CompileDownloadArchives(spec, registry)
			if count == 5001 {
				if err == nil || nodes != nil {
					t.Fatal("oversized command compiled")
				}
				return
			}
			if err != nil || len(nodes) != 1 {
				t.Fatal("full command refused", err)
			}
			sum := sha256.Sum256(nodes[0].Params)
			params := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			task, err := PrepareDownloadArchiveTask(PublishedDownloadArchive{Node: nodes[0], Params: params})
			if err != nil || len(task.Inputs) != 5000 || task.Params != params {
				t.Fatal("wire shape changed", err)
			}
			for _, input := range task.Inputs {
				if input.ID == params.ID {
					t.Fatal("params consumed a declared input slot")
				}
			}
		})
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

func TestArchiveMaximumTinyInventoryResourceProfileMatchesWorker(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]DownloadArchiveEntry, MaxDownloadItems)
	for index := range entries {
		entries[index] = DownloadArchiveEntry{Ordinal: int64(index),
			Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
			Bytes:    1, EntryName: fmt.Sprintf("%04d.jpg", index), ProducerTask: uuid.New()}
	}
	spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64)}
	for start := 0; start < len(entries); start += MaxDownloadArchiveInputs {
		end := start + MaxDownloadArchiveInputs
		if end > len(entries) {
			end = len(entries)
		}
		spec.Volumes = append(spec.Volumes, DownloadArchiveVolume{Number: len(spec.Volumes) + 1, Entries: entries[start:end]})
	}
	nodes, err := CompileDownloadArchives(spec, registry)
	if err != nil || len(nodes) != 1 {
		t.Fatal("maximum inventory failed compile", err)
	}
	count := 0
	for _, node := range nodes {
		sum := sha256.Sum256(node.Params)
		task, err := PrepareDownloadArchiveTask(PublishedDownloadArchive{Node: node,
			Params: review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}})
		if err != nil {
			t.Fatal(err)
		}
		if len(task.Inputs) > MaxDownloadArchiveInputs || len(task.Inputs) != len(node.Inputs) {
			t.Fatal("archive TaskCommand inputs exceed wire shape", len(task.Inputs))
		}
		estimate, err := ParseResourceEstimate(task.ExecutionProfile)
		if err != nil {
			t.Fatal(err)
		}
		workerReservation := node.InputBytes + 8*1048576 + 4096*int64(len(node.Inputs))
		if estimate.ScratchBytes < workerReservation || estimate.OutputBytes < workerReservation {
			t.Fatal("backend under-admits worker archive reservation", estimate, workerReservation)
		}
		count += len(task.Inputs)
	}
	if count != MaxDownloadItems {
		t.Fatal("inventory truncated")
	}
	spec.Volumes = []DownloadArchiveVolume{{Number: 1, Entries: append(entries, DownloadArchiveEntry{Ordinal: 5000})}}
	if nodes, err := CompileDownloadArchives(spec, registry); err == nil || nodes != nil {
		t.Fatal("oversized command input inventory compiled")
	}
}

func TestCompileDownloadArchiveFrozenNameParityWithWorker(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		valid bool
	}{
		{"frame.jpg", true}, {"Сватба 001.jpg", true}, {"résumé.jpg", true},
		{"", false}, {".", false}, {"..", false}, {"../frame.jpg", false},
		{"folder/frame.jpg", false}, {"folder\\frame.jpg", false},
		{"frame\x00.jpg", false}, {"frame\n.jpg", false}, {"frame\x7f.jpg", false},
		{"manifest.json", false}, {"MANIFEST.JSON", false}, {"ｍanifest.json", false},
		{"e\u0301.jpg", false}, {string([]byte{0xff, '.', 'j', 'p', 'g'}), false},
		{strings.Repeat("x", 181), false},
	}
	for _, test := range cases {
		t.Run(fmt.Sprintf("%q", test.name), func(t *testing.T) {
			spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(),
				SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 1,
					Entries: []DownloadArchiveEntry{{Ordinal: 0, Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
						Bytes: 1, EntryName: test.name, ProducerTask: uuid.New()}}}}}
			nodes, err := CompileDownloadArchives(spec, registry)
			if test.valid {
				if err != nil || len(nodes) != 1 {
					t.Fatal("valid name refused", err)
				}
			} else if err == nil || nodes != nil {
				t.Fatal("unsafe name compiled", nodes)
			}
		})
	}
}

func TestArchiveCompilerInventoryIsBoundedAcrossAllVolumes(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	makeEntry := func(ordinal int64, name string) DownloadArchiveEntry {
		return DownloadArchiveEntry{Ordinal: ordinal, Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)},
			Bytes: 1, EntryName: name, ProducerTask: uuid.New()}
	}
	for _, duplicateAcrossVolumes := range []bool{false, true} {
		spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64)}
		if duplicateAcrossVolumes {
			spec.Volumes = []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{makeEntry(0, "a.jpg")}},
				{Number: 2, Entries: []DownloadArchiveEntry{makeEntry(0, "b.jpg")}}}
		} else {
			spec.Volumes = []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{makeEntry(0, "a.jpg"), makeEntry(0, "b.jpg")}}}
		}
		if nodes, err := CompileDownloadArchives(spec, registry); err == nil || nodes != nil {
			t.Fatal("duplicate ordinal accepted")
		}
	}
	// Archive-only inventory may have gaps occupied by explicit single-file
	// deliveries. Those gaps must not require renumbering snapshot ordinals.
	spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64),
		Volumes: []DownloadArchiveVolume{{Number: 1, Entries: []DownloadArchiveEntry{makeEntry(0, "a.jpg")}},
			{Number: 3, Entries: []DownloadArchiveEntry{makeEntry(2, "b.jpg")}}}}
	if nodes, err := CompileDownloadArchives(spec, registry); err != nil || len(nodes) != 2 {
		t.Fatal("valid single-file gaps refused", err)
	}
	over := make([]DownloadArchiveEntry, MaxDownloadItems/2+1)
	for index := range over {
		over[index] = makeEntry(int64(index), fmt.Sprintf("%04d.jpg", index))
	}
	spec.Volumes = []DownloadArchiveVolume{{Number: 1, Entries: over}, {Number: 2, Entries: over}}
	if nodes, err := CompileDownloadArchives(spec, registry); err == nil || nodes != nil {
		t.Fatal("aggregate inventory bound bypassed")
	}
}
