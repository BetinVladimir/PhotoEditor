package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"photoeditor/backend/internal/archivecontract"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type DownloadArchiveEntry struct {
	Ordinal      int64
	Artifact     review.ArtifactRef
	Bytes        int64
	EntryName    string
	ProducerTask uuid.UUID
}
type DownloadArchiveVolume struct {
	Number  int
	Entries []DownloadArchiveEntry
}
type DownloadArchiveSpec struct {
	RunID, SnapshotID uuid.UUID
	SnapshotDigest    string
	Volumes           []DownloadArchiveVolume
}
type CompiledDownloadArchive struct {
	ID                                     uuid.UUID
	Volume                                 int
	Key, Operation, Version, ResourceClass string
	TimeoutSeconds                         int
	Inputs                                 []review.ArtifactRef
	Params                                 []byte
	InputBytes                             int64
	Producers                              []uuid.UUID
}
type archiveParamEntry struct {
	Artifact  review.ArtifactRef `json:"artifact"`
	Bytes     int64              `json:"bytes"`
	EntryName string             `json:"entry_name"`
}
type archiveParams struct {
	SnapshotID     uuid.UUID           `json:"snapshot_id"`
	SnapshotDigest string              `json:"snapshot_digest"`
	Volume         int                 `json:"volume_no"`
	Entries        []archiveParamEntry `json:"entries"`
}

// Validate the frozen name without normalizing it: renaming after planning
// would invalidate collision decisions and the immutable manifest mapping.
func validArchiveEntryName(name string) bool {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) ||
		len([]byte(name)) > maxEntryNameBytes || strings.ContainsAny(name, "/\\") ||
		norm.NFKC.String(name) != name || cases.Fold().String(name) == "manifest.json" {
		return false
	}
	for _, char := range name {
		if char < 32 || char == 127 {
			return false
		}
	}
	return true
}

func CompileDownloadArchives(spec DownloadArchiveSpec, registry Registry) ([]CompiledDownloadArchive, error) {
	fail := func() ([]CompiledDownloadArchive, error) {
		return nil, errors.New("invalid download archive specification")
	}
	if spec.RunID == uuid.Nil || spec.SnapshotID == uuid.Nil || domainDigestInvalid(spec.SnapshotDigest) || len(spec.Volumes) > MaxDownloadItems {
		return fail()
	}
	op, ok := registry["archive.build"]
	if !ok || op.Version != "1" || op.ResourceClass != "archive" || op.Model != nil {
		return fail()
	}
	totalEntries := 0
	for _, volume := range spec.Volumes {
		if len(volume.Entries) < 1 || len(volume.Entries) > MaxDownloadItems-totalEntries {
			return fail()
		}
		totalEntries += len(volume.Entries)
	}
	seenOrdinals := make(map[int64]bool, totalEntries)
	volumes := append([]DownloadArchiveVolume(nil), spec.Volumes...)
	sort.Slice(volumes, func(i, j int) bool { return volumes[i].Number < volumes[j].Number })
	out := make([]CompiledDownloadArchive, 0, len(volumes))
	previous := 0
	for _, volume := range volumes {
		if volume.Number < 1 || volume.Number > 5000 || volume.Number <= previous || len(volume.Entries) < 1 || len(volume.Entries) > MaxDownloadArchiveInputs {
			return fail()
		}
		previous = volume.Number
		inputs := make([]review.ArtifactRef, 0, len(volume.Entries))
		params := make([]archiveParamEntry, 0, len(volume.Entries))
		producers := make([]uuid.UUID, 0, len(volume.Entries))
		names := map[string]bool{}
		var total int64
		manifestBytes := 512
		for _, entry := range volume.Entries {
			nameKey := cases.Fold().String(entry.EntryName)
			if entry.Ordinal < 0 || entry.Ordinal >= MaxDownloadItems || seenOrdinals[entry.Ordinal] || !validExpansionRef(entry.Artifact) || entry.Bytes < 1 || entry.Bytes > DefaultVolumeByteLimit || !validArchiveEntryName(entry.EntryName) || entry.ProducerTask == uuid.Nil || names[nameKey] {
				return fail()
			}
			manifestEntry, err := archiveManifestEntryBytes(entry.Artifact, entry.Bytes, entry.EntryName)
			if err != nil || len(manifestEntry)+1 > archivecontract.DocumentBytes-manifestBytes {
				return fail()
			}
			manifestBytes += len(manifestEntry) + 1
			names[nameKey] = true
			seenOrdinals[entry.Ordinal] = true
			if total > DefaultVolumeByteLimit-entry.Bytes {
				return fail()
			}
			total += entry.Bytes
			inputs = append(inputs, entry.Artifact)
			params = append(params, archiveParamEntry{entry.Artifact, entry.Bytes, entry.EntryName})
			producers = append(producers, entry.ProducerTask)
		}
		raw, err := canonical.Marshal(archiveParams{spec.SnapshotID, spec.SnapshotDigest, volume.Number, params})
		if err != nil || len(raw) > archivecontract.DocumentBytes {
			return fail()
		}
		key := fmt.Sprintf("archive.build/%04d", volume.Number)
		out = append(out, CompiledDownloadArchive{ID: uuid.NewSHA1(spec.RunID, []byte(key)), Volume: volume.Number, Key: key, Operation: op.Name, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds, Inputs: inputs, Params: raw, InputBytes: total, Producers: producers})
	}
	return out, nil
}

type DownloadArchiveRepository struct{ DB artifactjson.RowQuerier }

func (r DownloadArchiveRepository) Register(ctx context.Context, tenant uuid.UUID, spec DownloadArchiveSpec) ([]CompiledDownloadArchive, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.DB == nil || tenant == uuid.Nil {
		return nil, errors.New("invalid archive registration scope")
	}
	registry, err := LoadRegistry()
	if err != nil {
		return nil, err
	}
	nodes, err := CompileDownloadArchives(spec, registry)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(n.Params)
		inputs, _ := canonical.Marshal(n.Inputs)
		producers, _ := canonical.Marshal(n.Producers)
		var ok bool
		err = r.DB.QueryRow(ctx, "SELECT photo.register_download_archive_plan($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenant, spec.RunID, n.ID, n.Volume, n.Key, inputs, producers, hex.EncodeToString(sum[:]), append([]byte(nil), n.Params...)).Scan(&ok)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("download archive plan conflict")
		}
	}
	return nodes, nil
}

type PublishedDownloadArchive struct {
	Node   CompiledDownloadArchive
	Params review.ArtifactRef
}
type DownloadArchiveBuilder struct {
	Plans     DownloadArchiveRepository
	Publisher ContractPublisher
}

func (b DownloadArchiveBuilder) Publish(ctx context.Context, tenant uuid.UUID, spec DownloadArchiveSpec) ([]PublishedDownloadArchive, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.Publisher == nil || b.Plans.DB == nil {
		return nil, errors.New("archive publisher unavailable")
	}
	nodes, err := b.Plans.Register(ctx, tenant, spec)
	if err != nil {
		return nil, err
	}
	out := make([]PublishedDownloadArchive, 0, len(nodes))
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(n.Params)
		ref, e := b.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: spec.RunID, Owner: n.ID, Purpose: "task_params", DownloadArchive: true}, append([]byte(nil), n.Params...))
		if e != nil {
			return nil, e
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if ref.ID == uuid.Nil || ref.SHA256 != hex.EncodeToString(sum[:]) {
			return nil, errors.New("archive params integrity failure")
		}
		out = append(out, PublishedDownloadArchive{n, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}})
	}
	return out, nil
}

func PrepareDownloadArchiveTask(entry PublishedDownloadArchive) (PreparedTask, error) {
	n := entry.Node
	if n.Operation != "archive.build" || n.ResourceClass != "archive" || !validExpansionRef(entry.Params) || n.InputBytes < 1 || n.InputBytes > DefaultVolumeByteLimit || len(n.Inputs) < 1 || len(n.Inputs) > MaxDownloadArchiveInputs {
		return PreparedTask{}, errors.New("invalid archive task")
	}
	sum := sha256.Sum256(n.Params)
	if entry.Params.SHA256 != hex.EncodeToString(sum[:]) {
		return PreparedTask{}, errors.New("archive params hash mismatch")
	}
	// Match worker payload + per-entry ZIP/manifest accounting, retaining
	// another 8MiB for the separately staged external manifest.
	output := n.InputBytes + 2*archivecontract.DocumentBytes + archivecontract.ZIPEntryOverheadBytes*int64(len(n.Inputs))
	profile, err := canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "byte", ResourceEstimate{RAMBytes: 1024 * 1024 * 1024, ScratchBytes: output, OutputBytes: output, PixelCount: 0}})
	if err != nil {
		return PreparedTask{}, err
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return PreparedTask{}, err
	}
	digest, err := repairInputDigest(n.Operation, n.Version, entry.Params, n.Inputs)
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparedTask{ID: n.ID, NodeKey: n.Key, Operation: n.Operation, Version: n.Version, ResourceClass: n.ResourceClass, TimeoutSeconds: n.TimeoutSeconds, Params: entry.Params, Inputs: n.Inputs, InputDigest: digest, ExecutionProfile: profile}, nil
}
