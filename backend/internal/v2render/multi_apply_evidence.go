package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// applyEvidence consumes completed byte verification, without inventing a
// scheduler attempt/fence. SQL repeats the authoritative saved producer fence.
func (v MultiResultContent) applyEvidence(tenant, project, producer uuid.UUID, binding AdoptedMultiBinding) ([]byte, []byte, []byte, error) {
	if !v.valid || v.tenant != tenant || v.project != project || v.documents.binding != binding || validateAdoptedMultiBinding(binding, producer, binding.Operation, binding.Result) != nil {
		return nil, nil, nil, v2input.ErrInvalid
	}
	if binding.Replacement!=(MultiReplacementBindingIdentity{})&&(binding.Replacement.Tenant!=tenant||binding.Replacement.Project!=project){return nil,nil,nil,v2input.ErrInvalid}
	root, err := canonical.Marshal(v.documents.root)
	if err != nil {
		return nil, nil, nil, err
	}
	sum := sha256.Sum256(root)
	if hex.EncodeToString(sum[:]) != binding.Result.SHA256 {
		return nil, nil, nil, v2input.ErrInvalid
	}
	witnesses, err := v.outputWitnesses(binding)
	if err != nil {
		return nil, nil, nil, err
	}
	return root, []byte(v.documents.image), witnesses, nil
}
