package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
	"strconv"
)

type AdoptedMultiOriginalInventorySource interface {
	LoadOriginalInventory(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, AdoptedMultiBinding, string) (MultiOriginalInventory, error)
}

// Owns verified original JSON and its exact last-checked locator inventory.
// NPY bytes and ICC installation are not proved by these metadata documents.
type VerifiedMultiOriginals struct {
	binding   AdoptedMultiBinding
	documents []string
	inventory MultiOriginalInventory
}

func (v VerifiedMultiOriginals) Documents() [][]byte {
	out := make([][]byte, len(v.documents))
	for i, raw := range v.documents {
		out[i] = []byte(raw)
	}
	return out
}
func (v VerifiedMultiOriginals) Images() ([]review.WorkingImage, error) {
	return v2optical.VerifyMultiWorkingImages(v.binding.Operation, []byte(v.binding.ParamsJCS), v.Documents())
}
func (v VerifiedMultiOriginals) Inventory() MultiOriginalInventory {
	return MultiOriginalInventory{binding: v.inventory.binding, root: v.inventory.root, bucket: v.inventory.bucket, items: append([]MultiOriginalObject(nil), v.inventory.items...)}
}

func LoadAdoptedMultiOriginals(ctx context.Context, source AdoptedMultiOriginalInventorySource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, binding AdoptedMultiBinding, bucket string) (VerifiedMultiOriginals, error) {
	var empty VerifiedMultiOriginals
	if source == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || bucket == "" {
		return empty, v2input.ErrInvalid
	}
	if err := validateAdoptedMultiBinding(binding, producer, binding.Operation, binding.Result); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	inv, err := source.LoadOriginalInventory(ctx, who, tenant, project, producer, binding, bucket)
	if err != nil {
		return empty, err
	}
	inv = inv.owned()
	if err := validateMultiOriginalInventory(inv, binding, producer, bucket); err != nil {
		return empty, err
	}
	frames, _, _, err := binding.Inputs()
	if err != nil {
		return empty, err
	}
	raws := make([][]byte, len(frames))
	for _, item := range inv.items {
		if item.Role != "WorkingImage" {
			continue
		}
		raw, err := readMultiOriginalJSON(ctx, objects, item.Object, item.Ref)
		if err != nil {
			return empty, err
		}
		raws[item.Ordinal] = raw
	}
	images, err := v2optical.VerifyMultiWorkingImages(binding.Operation, []byte(binding.ParamsJCS), raws)
	if err != nil {
		return empty, err
	}
	if err := verifyMultiOriginalInventoryEdges(inv, frames, images); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	current, err := source.LoadOriginalInventory(ctx, who, tenant, project, producer, binding, bucket)
	if err != nil {
		return empty, err
	}
	if !inv.equal(current) {
		return empty, v2input.ErrExpired
	}
	documents := make([]string, len(raws))
	for i, raw := range raws {
		documents[i] = string(raw)
	}
	return VerifiedMultiOriginals{binding: binding, documents: documents, inventory: inv}, nil
}
func readMultiOriginalJSON(ctx context.Context, objects v2input.SessionAnalysisObjectReader, obj objectstore.Object, ref v2input.Ref) ([]byte, error) {
	if !validMultiChildObject(obj, ref, "application/json", 1048576, false) {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	body, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return nil, v2input.ErrInvalid
	}
	return bytes.Clone(body), nil
}
func verifyMultiOriginalInventoryEdges(inv MultiOriginalInventory, frames []v2input.SessionVersionBinding, images []review.WorkingImage) error {
	if len(frames) != len(images) {
		return v2input.ErrInvalid
	}
	expected := make([]map[string]v2input.Ref, len(frames))
	count := 0
	for i, image := range images {
		expected[i] = map[string]v2input.Ref{"WorkingImage": frames[i].Source, "WorkingPixels": {ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}}
		if image.Alpha != nil {
			expected[i]["WorkingAlpha"] = v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}
		}
		var chain struct {
			Steps []struct {
				Kind    string       `json:"kind"`
				Inverse *v2input.Ref `json:"inverse_field"`
			} `json:"steps"`
		}
		if json.Unmarshal(image.TransformChain, &chain) != nil {
			return v2input.ErrInvalid
		}
		for step, value := range chain.Steps {
			if value.Kind == "dense_warp" {
				if value.Inverse == nil {
					return v2input.ErrInvalid
				}
				expected[i]["InverseField/"+strconv.Itoa(step)] = *value.Inverse
			}
		}
		count += len(expected[i])
	}
	if count != len(inv.items) {
		return v2input.ErrInvalid
	}
	for _, item := range inv.items {
		if item.Ordinal < 0 || item.Ordinal >= len(expected) || expected[item.Ordinal][item.Role] != item.Ref {
			return v2input.ErrInvalid
		}
	}
	return nil
}
