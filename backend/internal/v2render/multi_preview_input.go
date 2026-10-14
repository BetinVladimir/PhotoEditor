package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PreviewDocuments exposes owned exact root/image bytes only from a complete
// content read with matching tenant/project/frozen producer binding. It grants
// no admission lease; scheduler SQL must recheck the attempt/fence and closure.
func (v MultiResultContent) PreviewDocuments(source MultiPreviewSource) ([]byte, []byte, error) {
	if !v.valid || validateMultiPreviewSource(source) != nil || v.tenant != source.Tenant || v.project != source.Project || v.documents.binding != source.Binding {
		return nil, nil, v2input.ErrInvalid
	}
	root, err := canonical.Marshal(v.documents.root)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(root)
	if hex.EncodeToString(sum[:]) != source.Binding.Result.SHA256 {
		return nil, nil, v2input.ErrInvalid
	}
	return root, []byte(v.documents.image), nil
}
