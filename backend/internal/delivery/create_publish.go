package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CreateDocumentPublisher must bind each reservation to the durable pending
// intent, preserve the supplied artifact ID and authorize current actor scope.
// A method call must not retain an SQL transaction across object storage IO.
type CreateDocumentPublisher interface {
	PublishCreateDocument(context.Context, CreateIdentity, CreateDocument) (v2input.Ref, error)
}

// PublishDocuments publishes only after all supplied intents have been checked.
// It does not create/activate a graph. A failed attempt leaves immutable documents
// recoverable through the publisher's existing reservation/version lifecycle.
func (i CreateIntent) PublishDocuments(ctx context.Context, docs CreateDocuments, publisher CreateDocumentPublisher) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	invalid := func() error { return domain.NewError("DELIVERY_DOCUMENT_INVALID", 500) }
	if publisher == nil || i.identity.Run == uuid.Nil || docs.Plan.TaskID() != i.identity.PrepareTask || len(docs.Presets) < 1 || len(docs.Presets) > 16 {
		return invalid()
	}
	// Own the full publication sequence before callbacks can yield or mutate input.
	sequence := []CreateDocument{docs.Snapshot}
	sequence = append(sequence, docs.Presets...)
	sequence = append(sequence, docs.Policy, docs.Frozen, docs.Params)
	seen := map[uuid.UUID]bool{}
	for index, doc := range sequence {
		limit := 1 << 20
		expected := "v30_delivery_preset"
		if index == 0 {
			expected = "v30_delivery_snapshot"
			limit = 8 << 20
		}
		if index == len(sequence)-3 {
			expected = "v30_delivery_policy"
		}
		if index == len(sequence)-2 {
			expected = "v30_delivery_input"
		}
		if index == len(sequence)-1 {
			expected = "task_params"
			limit = 65536
		}
		if doc.Purpose != expected || doc.Ref.ArtifactID == uuid.Nil || seen[doc.Ref.ArtifactID] || len(doc.Body) < 2 || len(doc.Body) > limit || digest(doc.Body) != doc.Ref.SHA256 {
			return invalid()
		}
		canonicalBody, err := canonical.Bytes(doc.Body)
		if err != nil || !bytes.Equal(canonicalBody, doc.Body) {
			return invalid()
		}
		seen[doc.Ref.ArtifactID] = true
		sequence[index].Body = append([]byte(nil), doc.Body...)
	}
	if docs.Policy.Ref.ArtifactID != i.identity.Policy || docs.Frozen.Ref.ArtifactID != i.identity.FrozenInput || docs.Params.Ref.ArtifactID != i.identity.Params || !bytes.Equal(docs.Params.Body, docs.Plan.ParamsJCS()) {
		return invalid()
	}

	var proof PrepareProof
	if json.Unmarshal(docs.Plan.ProofJCS(), &proof) != nil || proof.ProjectID != i.request.ProjectID || proof.FrozenInputJCS != string(docs.Frozen.Body) || proof.SnapshotJCS != string(docs.Snapshot.Body) || docs.Plan.input != docs.Frozen.Ref {
		return invalid()
	}
	var frozen FrozenInput
	if json.Unmarshal(docs.Frozen.Body, &frozen) != nil || frozen.Snapshot != docs.Snapshot.Ref || len(frozen.Presets) != len(docs.Presets) || len(proof.ConfigsJCS) != len(docs.Presets) {
		return invalid()
	}
	policyBody, err := canonical.Marshal(frozen.Policy)
	if err != nil || !bytes.Equal(policyBody, docs.Policy.Body) {
		return invalid()
	}
	configs := map[uuid.UUID]string{}
	for _, config := range proof.ConfigsJCS {
		if _, exists := configs[config.PresetID]; exists {
			return invalid()
		}
		configs[config.PresetID] = config.ConfigJCS
	}
	published := map[v2input.Ref][]byte{}
	for _, doc := range docs.Presets {
		published[doc.Ref] = doc.Body
	}
	for _, preset := range frozen.Presets {
		body, ok := published[preset.Config]
		if !ok || string(body) != configs[preset.PresetID] || preset.PresetSHA != preset.Config.SHA256 {
			return invalid()
		}
	}
	for _, doc := range sequence {
		if err := ctx.Err(); err != nil {
			return err
		}
		expected := doc.Ref
		actual, err := publisher.PublishCreateDocument(ctx, i.identity, doc)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if actual != expected {
			return domain.NewError("DELIVERY_PUBLICATION_BINDING_INVALID", 500)
		}
	}
	return ctx.Err()
}
