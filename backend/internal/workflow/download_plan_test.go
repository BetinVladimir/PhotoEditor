package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func downloadSource(ordinal int64, bytes int64, name, hash string) DownloadSource {
	return DownloadSource{Ordinal: ordinal, AssetVersionID: uuid.New(), ArtifactID: uuid.New(), SHA256: hash, Bytes: bytes, Filename: name}
}

func TestDownloadPlannerTransportBoundaries(t *testing.T) {
    for _, count := range []int{512, 513, 5000} {
        t.Run(fmt.Sprint(count), func(t *testing.T) {
            sources := make([]DownloadSource, count)
            for i := range sources { sources[i] = downloadSource(int64(i), 1, fmt.Sprintf("%04d.jpg", i), fmt.Sprintf("%064x", i+1)) }
            plan, err := PlanDownloadVolumesWithLimits(sources, DefaultVolumeByteLimit, 5000)
            if err != nil || len(plan) != count { t.Fatal("inventory lost", err, len(plan)) }
            sizes := map[int]int{}
            for i, item := range plan {
                if item.Ordinal != int64(i) || item.ArtifactID != sources[i].ArtifactID { t.Fatal("selection changed", i) }
                sizes[item.Volume]++
                if sizes[item.Volume] > MaxDownloadArchiveInputs { t.Fatal("wire input ceiling exceeded") }
            }
            if len(sizes) != (count+511)/512 { t.Fatal("wrong split", sizes) }
            if sizes[1] != 512 { t.Fatal("unnecessarily reduced first volume", sizes) }
        })
    }
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

func TestDownloadPlanLongNamesSplitByParameterBudgetWithoutLosingInventory(t *testing.T) {
    items := make([]DownloadSource, MaxDownloadItems)
    for index := range items {
        items[index] = downloadSource(int64(index), 1,
            fmt.Sprintf("%04d_%s.jpg", index, strings.Repeat("x", 171)), strings.Repeat("a", 64))
    }
    plan, err := PlanDownloadVolumes(items, DefaultVolumeByteLimit)
    if err != nil { t.Fatal(err) }
    repeated, err := PlanDownloadVolumes(items, DefaultVolumeByteLimit)
    if err != nil || len(plan) != len(items) { t.Fatal("inventory lost", err) }
    volumes := []DownloadArchiveVolume{}
    for index, item := range plan {
        if item.Volume != repeated[index].Volume || item.EntryName != repeated[index].EntryName ||
            item.Ordinal != int64(index) || item.ArtifactID != items[index].ArtifactID || item.DeliveryKind != "zip" {
            t.Fatal("unstable or reordered plan", index)
        }
        if len(volumes) == 0 || volumes[len(volumes)-1].Number != item.Volume {
            volumes = append(volumes, DownloadArchiveVolume{Number: item.Volume})
        }
        last := &volumes[len(volumes)-1]
        last.Entries = append(last.Entries, DownloadArchiveEntry{Ordinal: item.Ordinal,
            Artifact: review.ArtifactRef{ID: item.ArtifactID, SHA256: item.SHA256},
            Bytes: item.Bytes, EntryName: item.EntryName, ProducerTask: uuid.New()})
    }
    if len(volumes) < 2 { t.Fatal("long names did not split") }
    registry, err := LoadRegistry()
    if err != nil { t.Fatal(err) }
    nodes, err := CompileDownloadArchives(DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(),
        SnapshotDigest: strings.Repeat("b", 64), Volumes: volumes}, registry)
    if err != nil { t.Fatal("planned volumes exceed transport budget", err) }
    count := 0
    for _, node := range nodes {
        count += len(node.Inputs)
        if len(node.Params) > 1048576 { t.Fatal("oversized archive params") }
    }
    if count != MaxDownloadItems { t.Fatal("compiled inventory lost", count) }
}

func TestDownloadPlanAlsoBoundsLargerExternalArchiveManifest(t *testing.T) {
    items:=make([]DownloadSource,MaxDownloadItems)
    for index:=range items { items[index]=downloadSource(int64(index),1,fmt.Sprintf("%04d.jpg",index),strings.Repeat("a",64)) }
    plan,err:=PlanDownloadVolumes(items,DefaultVolumeByteLimit)
    if err!=nil { t.Fatal(err) }
    volumes:=map[int]int{}
    for _,item:=range plan {
        entry,err:=archiveManifestEntryBytes(review.ArtifactRef{ID:item.ArtifactID,SHA256:item.SHA256},item.Bytes,item.EntryName)
        if err!=nil { t.Fatal(err) }
        if volumes[item.Volume]==0 { volumes[item.Volume]=512 }
        volumes[item.Volume]+=len(entry)+1
    }
    if len(volumes)<2 { t.Fatal("5000 short names did not split for manifest limit") }
    for _,bytes:=range volumes { if bytes>1048576 { t.Fatal("external manifest would exceed output staging",bytes) } }
    if len(plan)!=MaxDownloadItems { t.Fatal("inventory truncated") }
}
