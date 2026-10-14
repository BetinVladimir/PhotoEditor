package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type manualEvidencePin struct {
	Role       string    `json:"role"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	Version    string    `json:"version"`
	Bytes      int64     `json:"bytes"`
	Media      string    `json:"media_type"`
}

// marshalManualEvidencePins serializes only privately retained verified catalog
// objects. SQL must lock and compare these exact pins, not accept client JSON.
func marshalManualEvidencePins(proof manualPreparationProof, bucket string) ([]byte, error) {
	who := auth.Identity{UserID: proof.actor, SessionID: proof.session}
	if !validManualPreparationScope(proof.project, who, proof.tenant, proof.binding, proof) || bucket == "" {
		return nil, v2input.ErrInvalid
	}
	entries := []struct {
		role string
		pin  defectEvidenceObject
	}{
		{"rasterized_mask", proof.objects.Root}, {"task_params", proof.objects.Params}, {"mask_target", proof.objects.Pixel},
		{"mask_protected", proof.children[0]}, {"mask_target_png", proof.children[1]}, {"mask_protected_png", proof.children[2]},
	}
	if proof.vectors != nil {
		entries = append(entries, struct {
			role string
			pin  defectEvidenceObject
		}{"manual_vectors", *proof.vectors})
	}
	pins := make([]manualEvidencePin, 0, len(entries))
	seen := map[uuid.UUID]bool{proof.binding.Source.ArtifactID: true}
	for _, entry := range entries {
		p := entry.pin
		if p.ref.ArtifactID == uuid.Nil || seen[p.ref.ArtifactID] || p.object.Key == "" || p.object.VersionID == "" || p.object.VersionID == "null" || p.object.SHA256 != p.ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(p.ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		media, min, max := "application/json", int64(2), int64(1048576)
		if entry.role == "mask_target" || entry.role == "mask_protected" {
			media, min, max = "application/octet-stream", 32, 800065548
		}
		if entry.role == "mask_target_png" || entry.role == "mask_protected_png" {
			media, min, max = "image/png", 57, 613048576
		}
		if p.object.ContentType != media || p.object.Bytes < min || p.object.Bytes > max {
			return nil, v2input.ErrInvalid
		}
		seen[p.ref.ArtifactID] = true
		pins = append(pins, manualEvidencePin{entry.role, p.ref.ArtifactID, p.ref.SHA256, bucket, p.object.Key, p.object.VersionID, p.object.Bytes, media})
	}
	sort.Slice(pins, func(i, j int) bool { return pins[i].Role < pins[j].Role })
	return canonical.Marshal(pins)
}
