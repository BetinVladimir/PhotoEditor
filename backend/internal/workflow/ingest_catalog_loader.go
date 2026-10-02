package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type CatalogImage struct {
	Ordinal      int64
	Source       review.ArtifactRef
	DecodeTaskID uuid.UUID
	MetadataRef  review.ArtifactRef
	Metadata     IngestMetadata
	Outputs      DecodeOutputBinding
	Image        review.WorkingImage
}
type IngestCatalogBatch struct {
	Ready    bool
	Images   []CatalogImage
	Failures []IngestItemFailure
}
type IngestCatalogLoader struct {
	DB                 ActivationRows
	Objects            ArtifactOpener
	Bucket, WorkICCSHA string
}
type decodeObject struct {
	inspectedObject
	Role string `json:"role"`
}

func nullJSON(raw []byte) bool { return len(raw) == 0 || string(raw) == "null" }
func objectRef(o inspectedObject) review.ArtifactRef {
	return review.ArtifactRef{ID: o.ID, SHA256: o.SHA}
}
func validCatalogObject(o inspectedObject, bucket, mime string, max int64) bool {
	return validExpansionRef(objectRef(o)) && o.Bucket == bucket && o.Key != "" && o.Version != "" && o.Version != "null" && o.MediaType == mime && o.Bytes > 0 && o.Bytes <= max
}

// Load validates all database rows before object IO and withholds a batch while
// any decode is pending. Canonical producer attestation is supplied by SQL60;
// catalog commit must recheck those bindings under the run lock.
func (r IngestCatalogLoader) Load(ctx context.Context, tenant uuid.UUID, spec IngestSpec) (IngestCatalogBatch, error) {
	invalid := errors.New("invalid ingest catalog outcomes")
	fail := func() (IngestCatalogBatch, error) { return IngestCatalogBatch{}, invalid }
	if r.DB == nil || r.Objects == nil || r.Bucket == "" || tenant == uuid.Nil {
		return fail()
	}
	if _, err := domain.ParseDigest(r.WorkICCSHA); err != nil {
		return fail()
	}
	registry, err := LoadRegistry()
	if err != nil {
		return IngestCatalogBatch{}, err
	}
	if _, err = CompileIngest(spec, registry); err != nil {
		return fail()
	}
	expected := map[int64]review.ArtifactRef{}
	for _, i := range spec.Items {
		expected[i.Ordinal] = i.Source
	}
	rows, err := r.DB.Query(ctx, "SELECT ordinal,source_id,source_sha,outcome,failure_stage,failure_reason,decode_task_id,decode_state,metadata,outputs FROM photo.ingest_decode_outcomes($1,$2)", tenant, spec.RunID)
	if err != nil {
		return IngestCatalogBatch{}, err
	}
	defer rows.Close()
	type pendingImage struct {
		image              CatalogImage
		metadata, manifest inspectedObject
	}
	pending := []pendingImage{}
	seen := map[int64]bool{}
	waiting := false
	batch := IngestCatalogBatch{Ready: true, Images: []CatalogImage{}, Failures: []IngestItemFailure{}}
	for rows.Next() {
		var ordinal int64
		var source review.ArtifactRef
		var outcome string
		var stage, reason, state *string
		var task *uuid.UUID
		var metadataRaw, outputsRaw []byte
		if err = rows.Scan(&ordinal, &source.ID, &source.SHA256, &outcome, &stage, &reason, &task, &state, &metadataRaw, &outputsRaw); err != nil {
			return IngestCatalogBatch{}, err
		}
		if seen[ordinal] || len(seen) >= len(expected) || expected[ordinal] != source || !validExpansionRef(source) {
			return fail()
		}
		seen[ordinal] = true
		if outcome == "failed" {
			if stage == nil || reason == nil || (*stage != "asset.verify" && *stage != "asset.inspect") || (*reason != "failed" && *reason != "cancelled") || task != nil || state != nil || !nullJSON(metadataRaw) || !nullJSON(outputsRaw) {
				return fail()
			}
			batch.Failures = append(batch.Failures, IngestItemFailure{ordinal, source, *stage, *reason})
			continue
		}
		if outcome != "decode" || stage != nil || reason != nil || task == nil || *task == uuid.Nil || state == nil {
			return fail()
		}
		var metadata inspectedObject
		if len(metadataRaw) > 8192 || json.Unmarshal(metadataRaw, &metadata) != nil || !validCatalogObject(metadata, r.Bucket, "application/json", 1048576) {
			return fail()
		}
		if TaskState(*state) != Succeeded {
			if !nullJSON(outputsRaw) {
				return fail()
			}
			switch TaskState(*state) {
			case Blocked, Ready, Queued, Running, RetryWait:
				waiting = true
			case Failed, Cancelled:
				batch.Failures = append(batch.Failures, IngestItemFailure{ordinal, source, "asset.decode", *state})
			default:
				return fail()
			}
			continue
		}
		var outputs []decodeObject
		if len(outputsRaw) > 32768 || json.Unmarshal(outputsRaw, &outputs) != nil || len(outputs) < 2 || len(outputs) > 3 {
			return fail()
		}
		roles := map[string]inspectedObject{}
		ids := map[uuid.UUID]bool{}
		for _, o := range outputs {
			if _, exists := roles[o.Role]; exists || ids[o.ID] {
				return fail()
			}
			ids[o.ID] = true
			mime, max := "application/octet-stream", int64(2401048576)
			switch o.Role {
			case "working_image":
				mime, max = "application/json", 1048576
			case "working_rgb":
			case "alpha":
				max = 401048576
			default:
				return fail()
			}
			if !validCatalogObject(o.inspectedObject, r.Bucket, mime, max) {
				return fail()
			}
			roles[o.Role] = o.inspectedObject
		}
		manifest, mok := roles["working_image"]
		pixels, pok := roles["working_rgb"]
		if !mok || !pok {
			return fail()
		}
		refs := DecodeOutputBinding{Manifest: objectRef(manifest), Pixels: objectRef(pixels)}
		if alpha, ok := roles["alpha"]; ok {
			ref := objectRef(alpha)
			refs.Alpha = &ref
		}
		pending = append(pending, pendingImage{CatalogImage{Ordinal: ordinal, Source: source, DecodeTaskID: *task, MetadataRef: objectRef(metadata), Outputs: refs}, metadata, manifest})
	}
	if err = rows.Err(); err != nil {
		return IngestCatalogBatch{}, err
	}
	rows.Close()
	if len(seen) != len(expected) {
		return fail()
	}
	if waiting {
		return IngestCatalogBatch{Ready: false}, nil
	}
	for _, p := range pending {
		err = r.read(ctx, p.metadata, func(reader io.Reader) error {
			var e error
			p.image.Metadata, e = ReadIngestMetadata(ctx, reader, p.image.MetadataRef, p.image.Source)
			return e
		})
		if err != nil {
			return IngestCatalogBatch{}, err
		}
		err = r.read(ctx, p.manifest, func(reader io.Reader) error {
			var e error
			p.image.Image, e = ReadDecodedImage(ctx, reader, p.image.Outputs, p.image.Metadata, r.WorkICCSHA)
			return e
		})
		if err != nil {
			return IngestCatalogBatch{}, err
		}
		batch.Images = append(batch.Images, p.image)
	}
	sort.Slice(batch.Images, func(i, j int) bool { return batch.Images[i].Ordinal < batch.Images[j].Ordinal })
	sort.Slice(batch.Failures, func(i, j int) bool { return batch.Failures[i].Ordinal < batch.Failures[j].Ordinal })
	return batch, nil
}
func (r IngestCatalogLoader) read(ctx context.Context, o inspectedObject, read func(io.Reader) error) error {
	reader, err := r.Objects.Open(ctx, objectstore.Object{Key: o.Key, VersionID: o.Version, SHA256: o.SHA, Bytes: o.Bytes, ContentType: o.MediaType})
	if err != nil {
		return err
	}
	limited := &io.LimitedReader{R: reader, N: o.Bytes + 1}
	readErr := read(limited)
	closeErr := reader.Close()
	if readErr != nil {
		return readErr
	}
	if limited.N != 1 {
		return errors.New("catalog object size mismatch")
	}
	return closeErr
}
