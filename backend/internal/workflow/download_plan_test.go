package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func downloadSource(ordinal int64, bytes int64, name, hash string) DownloadSource {
	return DownloadSource{Ordinal: ordinal, AssetVersionID: uuid.New(), ArtifactID: uuid.New(), SHA256: hash, Bytes: bytes, Filename: name}
}

func TestPlanDownloadVolumesStablePackingAndSingles(t *testing.T) {
	items := []DownloadSource{
		downloadSource(0, 6, "a.jpg", strings.Repeat("a", 64)),
		downloadSource(1, 5, "b.jpg", strings.Repeat("b", 64)),
		downloadSource(2, 11, "c.jpg", strings.Repeat("c", 64)),
		downloadSource(3, 4, "d.jpg", strings.Repeat("d", 64)),
	}
	plan, err := PlanDownloadVolumes(items, 10)
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].Volume != 1 || plan[1].Volume != 2 || plan[2].Volume != 3 || plan[2].DeliveryKind != "single" || plan[3].Volume != 4 {
		t.Fatal("unexpected stable packing", plan)
	}
}

func TestPlanDownloadVolumesSanitizesAndDisambiguatesNames(t *testing.T) {
	items := []DownloadSource{
		downloadSource(0, 1, "../Photo.JPG", strings.Repeat("a", 64)),
		downloadSource(1, 1, "photo.jpg", strings.Repeat("b", 64)),
		downloadSource(2, 1, strings.Repeat("я", 200)+".jpg", strings.Repeat("c", 64)),
	}
	plan, err := PlanDownloadVolumes(items, 10)
	if err != nil {
		t.Fatal(err)
	}
	if plan[0].EntryName != "Photo.JPG" || plan[1].EntryName != "photo__bbbbbbbb.jpg" {
		t.Fatal("unsafe or unstable collision handling", plan)
	}
	for _, item := range plan {
		if len([]byte(item.EntryName)) > maxEntryNameBytes || strings.ContainsAny(item.EntryName, "/\\") {
			t.Fatal("invalid planned entry name", item.EntryName)
		}
	}
}

func TestPlanDownloadVolumesRejectsBrokenSnapshotOrder(t *testing.T) {
	if _, err := PlanDownloadVolumes([]DownloadSource{downloadSource(1, 1, "a.jpg", strings.Repeat("a", 64))}, 10); err == nil {
		t.Fatal("broken ordinal accepted")
	}
}

func TestPlanDownloadVolumesReservesManifestName(t *testing.T) {
	plan, err := PlanDownloadVolumes([]DownloadSource{downloadSource(0, 1, "manifest.json", strings.Repeat("a", 64))}, 10)
	if err != nil || plan[0].EntryName != "manifest__aaaaaaaa.json" {
		t.Fatal("reserved manifest name not disambiguated", plan, err)
	}
}

func TestPlanDownloadVolumesMaximumCardinalityHasNoOmissionsOrDuplicates(t *testing.T) {
	items := make([]DownloadSource, MaxDownloadItems)
	for index := range items {
		hash := fmt.Sprintf("%064x", index+1)
		// Every 997th item is deliberately oversized and must be a separate
		// delivery; the remaining synthetic sizes force multiple ZIP volumes.
		size := int64(3 * 1024 * 1024)
		if index%997 == 0 {
			size = 11 * 1024 * 1024
		}
		items[index] = downloadSource(int64(index), size, fmt.Sprintf("photo-%04d.jpg", index), hash)
	}
	plan, err := PlanDownloadVolumes(items, 10*1024*1024)
	if err != nil || len(plan) != len(items) {
		t.Fatal("maximum plan", len(plan), err)
	}
	seen := make(map[uuid.UUID]bool, len(plan))
	lastVolume := 0
	for index, item := range plan {
		if item.Ordinal != int64(index) || seen[item.ArtifactID] || item.Volume < lastVolume || item.Volume < 1 {
			t.Fatal("omission, duplicate, reorder or volume regression", index, item)
		}
		seen[item.ArtifactID] = true
		lastVolume = item.Volume
		if index%997 == 0 && item.DeliveryKind != "single" {
			t.Fatal("oversized item entered ZIP", index)
		}
	}
	if _, err = PlanDownloadVolumes(append(items, downloadSource(MaxDownloadItems, 1, "overflow.jpg", strings.Repeat("f", 64))), 10*1024*1024); err == nil {
		t.Fatal("snapshot cardinality limit bypassed")
	}
}
