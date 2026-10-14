package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// RestorePublishedTransport checks recovery metadata against the original
// qualified descriptor. It provides no SQL scope or physical object authority.
func (d NegativeDraft) RestorePublishedTransport(ctx context.Context, raw []byte, qualified VerifiedNegativeQualification, published v2input.Ref) (v2input.GraphTask, error) {
 return restoreNegativePublishedTransport(ctx,raw,func(ctx context.Context)([]byte,error){return d.Descriptor(ctx,qualified,published)})
}

// Shared byte/hash verifier. The loader must reconstruct a qualified descriptor;
// this helper alone supplies neither SQL scope nor physical input authority.
func restoreNegativePublishedTransport(ctx context.Context,raw []byte,loadDescriptor func(context.Context)([]byte,error))(v2input.GraphTask,error){
 return restoreOpticalPublishedTransport(ctx,raw,loadDescriptor,4)
}
func restoreOpticalPublishedTransport(ctx context.Context,raw []byte,loadDescriptor func(context.Context)([]byte,error),expectedRoots int)(v2input.GraphTask,error){
 return restoreBoundedOpticalPublishedTransport(ctx,raw,loadDescriptor,expectedRoots,1)
}
func restoreBoundedOpticalPublishedTransport(ctx context.Context,raw []byte,loadDescriptor func(context.Context)([]byte,error),expectedRoots,maxChunks int)(v2input.GraphTask,error){
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
	if err != nil || !bytes.Equal(exact, normal) || !validNegativeTransportRef(wire.Manifest) || len(wire.Chunks)<1 || len(wire.Chunks)>maxChunks || wire.Count<1 || wire.Count>4096 || len(wire.Chunks)!=(wire.Count+511)/512 {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := loadDescriptor(ctx)
	if err != nil {
		return zero, err
	}
	var frozen struct {
		Task    v2input.DraftTask `json:"task"`
		Entries []v2input.Entry   `json:"entries"`
	}
	if json.Unmarshal(descriptor, &frozen) != nil || len(frozen.Entries) != wire.Count {
		return zero, v2input.ErrInvalid
	}
	entriesSHA, err := canonical.Hash(frozen.Entries)
	if err != nil || entriesSHA != wire.SHA {
		return zero, v2input.ErrInvalid
	}
 seen:=map[uuid.UUID]bool{wire.Manifest.ArtifactID:true}
 for i,ref:=range wire.Chunks{
  if !validNegativeTransportRef(ref)||seen[ref.ArtifactID]{return zero,v2input.ErrInvalid};seen[ref.ArtifactID]=true
  end:=(i+1)*512;if end>len(frozen.Entries){end=len(frozen.Entries)}
  chunkSHA,err:=canonical.Hash(v2input.Chunk{SchemaVersion:1,Entries:frozen.Entries[i*512:end]})
  if err!=nil||chunkSHA!=ref.SHA256{return zero,v2input.ErrInvalid}
 }
	manifestSHA, err := canonical.Hash(v2input.Manifest{SchemaVersion: 1, TaskID: frozen.Task.ID, EntryCount: wire.Count, EntriesSHA: wire.SHA, Chunks: wire.Chunks})
	if err != nil || manifestSHA != wire.Manifest.SHA256 {
		return zero, v2input.ErrInvalid
	}
	var direct []v2input.Ref
	for _, entry := range frozen.Entries {
		if seen[entry.Ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if entry.Contract != "NpyArray" {
			direct = append(direct, entry.Ref)
		}
	}
 if len(direct)!=expectedRoots{return zero,v2input.ErrInvalid}
	entries := append([]v2input.Entry(nil), frozen.Entries...)
	return v2input.GraphTask{ID: frozen.Task.ID, Built: v2input.Built{Manifest: wire.Manifest, Chunks: append([]v2input.Ref(nil), wire.Chunks...), Count: wire.Count, SHA256: wire.SHA}, Direct: direct, Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &negativeEntryCursor{entries: append([]v2input.Entry(nil), entries...)}, nil
	}}, nil
}

func validNegativeTransportRef(ref v2input.Ref)bool{
 if ref.ArtifactID==uuid.Nil{return false};_,err:=domain.ParseDigest(ref.SHA256);return err==nil
}
