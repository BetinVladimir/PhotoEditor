package interchange

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// The database additionally resolves original source lineage and manifest inventory.
// This check rejects malformed certificates even when the publisher is bypassed.
func validateImportWarpOccurrence(scope ImportPreparedScope,ref v2input.Ref)error{
 if scope.Warp==nil{return nil}
 occurrence:=*scope.Warp
 validCanvas:=func(c review.Canvas)bool{return c.Width>=1&&c.Width<=200000000&&c.Height>=1&&c.Height<=200000000/c.Width&&c.Orientation==1&&importWarpSpace.MatchString(c.SpaceID)&&validImportRef(v2input.Ref{ArtifactID:scope.Entry,SHA256:c.SourceSHA256})}
 if scope.Contract!="DenseWarpField"||scope.Workflow==uuid.Nil||scope.Entry==uuid.Nil||scope.Binding.AssetVersionID==uuid.Nil||!validImportRef(scope.Binding.Source)||!validImportRef(v2input.Ref{ArtifactID:scope.Entry,SHA256:scope.Binding.CanvasHash})||!validImportRef(ref)||!validImportRef(occurrence.Original)||!validCanvas(occurrence.From)||!validCanvas(occurrence.To)||occurrence.From.SourceSHA256!=occurrence.To.SourceSHA256||ref.SHA256!=occurrence.Original.SHA256{return v2input.ErrInvalid}
 digest,err:=canonical.Hash(struct{From,To review.Canvas;Target v2input.SessionVersionBinding}{occurrence.From,occurrence.To,scope.Binding});if err!=nil{return err}
 expected:=uuid.NewSHA1(scope.Workflow,[]byte("interchange:warp:"+scope.Entry.String()+":"+occurrence.Original.ArtifactID.String()+":"+digest))
 if ref.ArtifactID!=expected{return v2input.ErrInvalid}
 return nil
}
