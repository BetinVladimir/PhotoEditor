package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// RestorePublishedTransport validates recovery metadata; SQL catalog authority
// and immutable object existence are independent prerequisites for using it.
func (p RelightAnalysisPlan) RestorePublishedTransport(ctx context.Context, raw []byte) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return zero, v2input.ErrExpired
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var wire struct {
		Manifest v2input.Ref   `json:"manifest"`
		Chunks   []v2input.Ref `json:"chunks"`
		Count    int           `json:"count"`
		SHA      string        `json:"sha256"`
	}
	if json.Unmarshal(normal, &wire) != nil {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(wire)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	return p.BindInputClosure(ctx, v2input.Built{Manifest: wire.Manifest, Chunks: wire.Chunks, Count: wire.Count, SHA256: wire.SHA})
}
