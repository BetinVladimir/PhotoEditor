package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

const authoredNativeReplayProducerSchemasSHA = "338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b"

type AuthoredNativeReplayObjects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}
type SQLAuthoredNativeReplayService struct {
	Scheduler *pgxpool.Pool
	Objects   AuthoredNativeReplayObjects
	Bucket    string
}

func resolveAuthoredNativeReplayInventory(ctx context.Context, who auth.Identity, catalog SQLAuthoredNativeReplayCatalog, requestRaw []byte) ([]v2input.Entry, error) {
	request, err := v2input.DecodeAuthoredNativeReplayRequest(requestRaw)
	if err != nil {
		return nil, err
	}
	children, err := v2input.AuthoredNativeReplayRequestChildren(requestRaw)
	if err != nil {
		return nil, err
	}
	anchor, err := catalog.originalBinding(ctx, who, request.Consumer.Tenant)
	if err != nil {
		return nil, err
	}
	nodes := make([]v2draft.LayerRef, len(children.Typed)+1)
	nodes[0] = v2draft.LayerRef{Path: "AuthoredNativeReplay.original_source", Ref: anchor.Source, Contract: "WorkingImage", CanvasHash: anchor.CanvasHash}
	for i, edge := range children.Typed {
		nodes[i+1] = v2draft.LayerRef{Path: "AuthoredNativeReplay.existing", Ref: edge.Ref, Contract: edge.Contract}
	}
	resolver := v2draft.ClosureResolver{Catalog: catalog, Objects: catalog.Objects}
	evidence, err := resolver.ResolveBoundProvenanceEvidence(ctx, who, request.Consumer.Tenant, nodes, anchor)
	if err != nil {
		return nil, err
	}
	// Opaque output observations are independently authorized/retained but never
	// parsed as JSON, inferred as Params, or allowed to downgrade a typed edge.
	entries := make(map[uuid.UUID]v2input.Entry, len(evidence.Entries)+len(children.Opaque)+1)
	for _, entry := range evidence.Entries {
		entries[entry.Ref.ArtifactID] = entry
	}
	for _, ref := range children.Opaque {
		if _, err := catalog.Lookup(ctx, who, request.Consumer.Tenant, v2draft.LayerRef{Ref: ref, Contract: "Artifact"}); err != nil {
			return nil, err
		}
		if prior, ok := entries[ref.ArtifactID]; ok {
			if prior.Ref != ref {
				return nil, v2input.ErrInvalid
			}
			continue
		}
		entries[ref.ArtifactID] = v2input.Entry{Ref: ref, Contract: "Artifact"}
	}
	_, _, _, requestRef, err := request.IDs()
	if err != nil {
		return nil, err
	}
	if _, exists := entries[requestRef.ArtifactID]; exists {
		return nil, v2input.ErrInvalid
	}
	// The initial pass only computes a blocked shell. This exact root is physically
	// published and reread before the second inventory pass and any graph seal.
	entries[requestRef.ArtifactID] = v2input.Entry{Ref: requestRef, Contract: "AuthoredNativeReplayRequest"}
	out := make([]v2input.Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].Ref.ArtifactID[:], out[j].Ref.ArtifactID[:]) < 0 })
	return out, nil
}

// Prepare stages BLOCKED immutable native work, publishes its actual request,
// repeats physical source resolution, and uses the existing public transport
// registration/graph seal. It never nominates a lease or canonical result.
// Missing installed namespace mounts cause a real refusal, not private fallback.
func (s SQLAuthoredNativeReplayService) Prepare(ctx context.Context, who auth.Identity, requestRaw []byte, expected v2qualification.Expected, report v2input.Ref) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	requestRaw = append([]byte(nil), requestRaw...)
	request, err := v2input.DecodeAuthoredNativeReplayRequest(requestRaw)
	if err != nil {
		return uuid.Nil, err
	}
	qualifier := SQLAuthoredNativeReplayQualification{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}
	proof, err := qualifier.Read(ctx, request.Consumer.Tenant, requestRaw, true, expected, report)
	if err != nil {
		return uuid.Nil, err
	}
	catalog, err := NewScopedAuthoredNativeReplayCatalog(ctx, who, s.Scheduler, s.Objects, s.Bucket, requestRaw)
	if err != nil {
		return uuid.Nil, err
	}
	entries, err := resolveAuthoredNativeReplayInventory(ctx, who, catalog, requestRaw)
	if err != nil {
		return uuid.Nil, err
	}
	nativeParams, err := catalog.read(ctx, who, request.Consumer.Tenant, request.Original.Task.Params)
	if err != nil {
		return uuid.Nil, err
	}
	frozen, err := v2input.CompileAuthoredNativeReplayTask(requestRaw, nativeParams, entries, authoredNativeReplayProducerSchemasSHA)
	if err != nil {
		return uuid.Nil, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return uuid.Nil, err
	}
	if err := authoredNativeReplayMeasuredBudget(draft.ExecutionProfile, proof.costs); err != nil {
		return uuid.Nil, err
	}
	if err := qualifier.Recheck(ctx, proof); err != nil {
		return uuid.Nil, err
	}
	var task uuid.UUID
	err = database.TenantTransaction(ctx, s.Scheduler, request.Consumer.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.begin_authored_native_replay($1,$2,$3::jsonb,$4,$5,$6,$7::jsonb,$8)`, request.Consumer.Tenant, string(requestRaw), proof.expected, proof.report.ArtifactID, proof.report.SHA256, draft.InputDigest, string(draft.ExecutionProfile), frozen.InventoryCount()).Scan(&task)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if task != draft.ID {
		return uuid.Nil, v2input.ErrConflict
	}
	publisher := AuthoredNativeReplayPublication{Scheduler: s.Scheduler, Objects: s.Objects, Bucket: s.Bucket}
	requestRef, err := publisher.Publish(ctx, request.Consumer.Tenant, requestRaw, "request", requestRaw)
	if err != nil {
		return uuid.Nil, err
	}
	actualRequest, err := catalog.read(ctx, who, request.Consumer.Tenant, requestRef)
	if err != nil {
		return uuid.Nil, err
	}
	if !bytes.Equal(actualRequest, requestRaw) {
		return uuid.Nil, v2input.ErrConflict
	}
	repeated, err := resolveAuthoredNativeReplayInventory(ctx, who, catalog, actualRequest)
	if err != nil {
		return uuid.Nil, err
	}
	a, _ := canonical.Marshal(entries)
	b, _ := canonical.Marshal(repeated)
	if !bytes.Equal(a, b) {
		return uuid.Nil, v2input.ErrConflict
	}
	run, _, _, _, err := request.IDs()
	if err != nil {
		return uuid.Nil, err
	}
	documents := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: s.Scheduler, Bucket: s.Bucket}, Objects: s.Objects}
	prepared, err := frozen.PublishTransport(ctx, v2input.ScopedPublisher{Documents: documents, Tenant: request.Consumer.Tenant, Run: run})
	if err != nil {
		return uuid.Nil, err
	}
	if err := qualifier.Recheck(ctx, proof); err != nil {
		return uuid.Nil, err
	}
	err = database.TenantTransaction(ctx, s.Scheduler, request.Consumer.Tenant, func(tx pgx.Tx) error {
		var current bool
		if err := tx.QueryRow(ctx, `SELECT photo.authored_native_replay_prepare_current($1,$2)`, request.Consumer.Tenant, task).Scan(&current); err != nil {
			return err
		}
		if !current {
			return v2input.ErrConflict
		}
		return v2input.SealGraphInTransaction(ctx, tx, request.Consumer.Tenant, run, []v2input.GraphTask{prepared.Graph})
	})
	if err != nil {
		return uuid.Nil, err
	}
	return task, nil
}

// The input index is independently live alongside native execution. Its budget
// cannot satisfy the measured native scratch requirement.
func authoredNativeReplayMeasuredBudget(raw []byte, costs v2input.SemanticQualifiedCosts) error {
	var profile struct {
		Index     int64 `json:"input_index_scratch_bytes"`
		Resources struct {
			RAM     int64 `json:"ram_bytes"`
			VRAM    int64 `json:"vram_bytes"`
			Scratch int64 `json:"scratch_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &profile) != nil || profile.Index < 16<<20 || profile.Index > profile.Resources.Scratch ||
		profile.Resources.RAM < costs.RAMBytes || profile.Resources.VRAM < costs.VRAMBytes ||
		profile.Resources.Scratch-profile.Index < costs.ScratchBytes {
		return v2qualification.ErrUnqualified
	}
	return nil
}
