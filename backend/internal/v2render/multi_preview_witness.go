package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// MultiPreviewObjectWitness describes a completed read, not current authority.
// Admission must independently resolve every exact typed path and compare the
// immutable locator again under the saved source attempt/fence.
type MultiPreviewObjectWitness struct {
	Ref           v2input.Ref `json:"ref"`
	Path          string      `json:"path"`
	Contract      string      `json:"contract"`
	ObjectKey     string      `json:"object_key"`
	ObjectVersion string      `json:"object_version"`
	SHA256        string      `json:"sha256"`
	Bytes         int64       `json:"bytes"`
	MediaType     string      `json:"media_type"`
}

// PreviewWitnesses returns owned canonical evidence for all verified output
// documents and arrays, including registration fields. It omits source objects:
// their exact inventory remains a separate required admission gate.
func (v MultiResultContent) PreviewWitnesses(source MultiPreviewSource) ([]byte, error) {
	if _, _, err := v.PreviewDocuments(source); err != nil {
		return nil, err
	}
	return v.outputWitnesses(source.Binding)
}

// outputWitnesses is private; callers first establish completed content scope.
func (v MultiResultContent) outputWitnesses(binding AdoptedMultiBinding) ([]byte, error) {
	if len(v.cataloguePins) == 0 {
		return nil, v2input.ErrInvalid
	}
	makeWitness := func(ref v2input.Ref, path, contract string, o objectstore.Object) (MultiPreviewObjectWitness, error) {
		if o.Key == "" || o.VersionID == "" || o.VersionID == "null" || o.SHA256 != ref.SHA256 || o.Bytes < 1 || o.ContentType == "" {
			return MultiPreviewObjectWitness{}, v2input.ErrInvalid
		}
		return MultiPreviewObjectWitness{Ref: ref, Path: path, Contract: contract, ObjectKey: o.Key, ObjectVersion: o.VersionID, SHA256: o.SHA256, Bytes: o.Bytes, MediaType: o.ContentType}, nil
	}
	root, err := makeWitness(binding.Result, "$", "ImageResult", v.documents.rootObject)
	if err != nil {
		return nil, err
	}
	witnesses := []MultiPreviewObjectWitness{root}
	seen := map[string]bool{}
	for _, pin := range v.cataloguePins {
		key := pin.ref.ArtifactID.String() + ":" + pin.ref.SHA256 + ":" + pin.path + ":" + pin.contract
		if seen[key] {
			return nil, v2input.ErrInvalid
		}
		seen[key] = true
		witness, err := makeWitness(pin.ref, pin.path, pin.contract, pin.object)
		if err != nil {
			return nil, err
		}
		witnesses = append(witnesses, witness)
	}
	return canonical.Marshal(witnesses)
}

// PreviewOriginalInventory serializes the complete physically verified source
// occurrence set in the same wire shape as the SQL inventory. Admission must
// compare it to a fresh inventory; this method grants no cached authorization.
func (v MultiResultContent) PreviewOriginalInventory(source MultiPreviewSource) ([]byte, error) {
	if _, _, err := v.PreviewDocuments(source); err != nil {
		return nil, err
	}
	return v.originalInventory(source.Binding, source.Producer)
}

func (v MultiResultContent) originalInventory(binding AdoptedMultiBinding, producer uuid.UUID) ([]byte, error) {
	inventory := v.documents.sources.originals.inventory
	if err := validateMultiOriginalInventory(inventory, binding, producer, inventory.bucket); err != nil {
		return nil, err
	}
	if inventory.root != v.documents.rootObject {
		return nil, v2input.ErrInvalid
	}
	wire := multiOriginalInventoryWire{SchemaVersion: 1, Binding: inventory.binding, Root: multiOriginalLocatorWire{Bucket: inventory.bucket, Key: inventory.root.Key, Version: inventory.root.VersionID, Bytes: inventory.root.Bytes, Media: inventory.root.ContentType}, Items: make([]multiOriginalItemWire, len(inventory.items))}
	for i, item := range inventory.items {
		wire.Items[i] = multiOriginalItemWire{Ordinal: item.Ordinal, Role: item.Role, Ref: item.Ref, Bucket: inventory.bucket, Key: item.Object.Key, Version: item.Object.VersionID, Bytes: item.Object.Bytes, Media: item.Object.ContentType}
	}
	return canonical.Marshal(wire)
}
