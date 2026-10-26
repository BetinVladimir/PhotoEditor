package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/archivecontract"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestArchive5000EscapedNamesOneVolumeHasBoundedDocsAndCommand(t *testing.T) {
	registry, e := LoadRegistry()
	if e != nil {
		t.Fatal(e)
	}
	entries := make([]DownloadArchiveEntry, 5000)
	for i := range entries {
		entries[i] = DownloadArchiveEntry{Ordinal: int64(i), Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Bytes: 1, EntryName: fmt.Sprintf("%04d_", i) + strings.Repeat("\"", 171) + ".jpg", ProducerTask: uuid.New()}
		if len(entries[i].EntryName) != 180 {
			t.Fatal("fixturebytes")
		}
	}
	spec := DownloadArchiveSpec{RunID: uuid.New(), SnapshotID: uuid.New(), SnapshotDigest: strings.Repeat("b", 64), Volumes: []DownloadArchiveVolume{{Number: 1, Entries: entries}}}
	nodes, e := CompileDownloadArchives(spec, registry)
	if e != nil || len(nodes) != 1 || len(nodes[0].Inputs) != 5000 {
		t.Fatal("fullvolume", e)
	}
	if len(nodes[0].Params) <= 1048576 || len(nodes[0].Params) > archivecontract.DocumentBytes {
		t.Fatal("documentbound", len(nodes[0].Params))
	}
	digest := sha256.Sum256(nodes[0].Params)
	params := review.ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(digest[:])}
	task, e := PrepareDownloadArchiveTask(PublishedDownloadArchive{Node: nodes[0], Params: params})
	if e != nil {
		t.Fatal(e)
	}
	estimate, e := ParseResourceEstimate(task.ExecutionProfile)
	want := int64(5000 + 2*archivecontract.DocumentBytes + 4096*5000)
	if e != nil || estimate.ScratchBytes != want || estimate.OutputBytes != want {
		t.Fatal("underadmission", estimate, e)
	}
	command, e := canonical.Marshal(map[string]any{"schema_version": 1, "event_id": uuid.New(), "tenant_id": uuid.New(), "run_id": spec.RunID, "task_id": task.ID, "attempt": 1, "operation": "archive.build", "operation_version": "1", "resource_class": "archive", "correlation_id": uuid.New(), "inputs": task.Inputs, "params": params, "model": nil, "timeout_seconds": 7200, "traceparent": ""})
	if e != nil || len(command) <= 65536 || len(command) > archivecontract.CommandBytes {
		t.Fatal("wirebound", len(command), e)
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(command, &decoded) != nil {
		t.Fatal("wirejson")
	}
	t.Logf("actual5000 params=%d command=%d; technicalserialization only", len(nodes[0].Params), len(command))
}
func TestArchiveEveryValidNameConservativeEscapingFits8MiB(t *testing.T) {
	ref := review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	manifest, e := archiveManifestEntryBytes(ref, DefaultVolumeByteLimit, "")
	if e != nil {
		t.Fatal(e)
	}
	params, e := canonical.Marshal(archiveParamEntry{ref, DefaultVolumeByteLimit, ""})
	if e != nil {
		t.Fatal(e)
	}
	// Even sixJSONbytes per allowed UTF8byte dominates quote escaping, HTML
	// escapes and unicode escapes. Fixedentry includes maxordinal/bytes/UUID/SHA.
	for _, fixed := range []int{len(manifest), len(params)} {
		bound := 512 + MaxDownloadItems*(fixed+6*maxEntryNameBytes+1)
		if bound > archivecontract.DocumentBytes {
			t.Fatal("fullvalidcontractcouldexceed8MiB", bound)
		}
	}
}
