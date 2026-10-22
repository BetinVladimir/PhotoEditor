package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"

 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// AuthoredCurrentPerspectiveLines is content evidence from the separately
// protected current-source detector protocol. The coordinator/SQL must still
// bind its task, qualification, adopted root and immutable segments publication.
// This compiler neither admits that producer nor turns content into authority.
type AuthoredCurrentPerspectiveLines struct {
 AuthoredLines v2input.Ref `json:"authored_lines"`
 CurrentLines v2input.Ref `json:"current_lines"`
 DetectedRoot v2input.Ref `json:"detected_lines_root"`
 Producer AuthoredPhaseTask `json:"lines_producer"`
}

// MaterializeAuthoredCurrentPerspectiveLines changes only auto Params.Lines
// after the ordinary exact-layer materializer has selected the original Params
// and changed Base. Manual geometry cannot acquire this witness or change mode.
func MaterializeAuthoredCurrentPerspectiveLines(selected AuthoredGeometryMaterialization,originalRaw,detectedRaw,segmentsRaw []byte,witness AuthoredCurrentPerspectiveLines)(AuthoredGeometryMaterialization,error){
 fail:=func()(AuthoredGeometryMaterialization,error){return AuthoredGeometryMaterialization{},v2input.ErrInvalid}
 if selected.Operation!="optical.perspective"||selected.Instruction.Kind!="geometry"{return fail()}
 original,err:=v2optical.ParsePerspectiveParams(originalRaw);if err!=nil||original.Mode!="auto"||original.Lines==nil||*original.Lines!=witness.AuthoredLines{return fail()}
 sum:=sha256.Sum256(originalRaw);if hex.EncodeToString(sum[:])!=selected.Params.Authored.SHA256{return fail()}
 current,err:=v2optical.ParsePerspectiveParams(selected.Params.Body);if err!=nil||current.Base!=selected.Params.Source{return fail()}
 // Reconstruct the ordinary Base-only transformation independently. This
 // prevents a caller changing strength/crop/mode/corners before rebinding Lines.
 original.Base=current.Base
 baseline,_,err:=original.Compile();if err!=nil||!bytes.Equal(baseline,selected.Params.Body){return fail()}
 producer:=witness.Producer
 if producer.Operation!="optical.perspective_lines"||producer.Version!="1"||producer.RunID==uuid.Nil||producer.TaskID==uuid.Nil||producer.ResultID==uuid.Nil||producer.Attempt<1||producer.Fence<1||len(producer.Outputs)!=1||producer.Outputs[0]!=witness.DetectedRoot{return fail()}
 if !validLayerArtifactRef(witness.CurrentLines)||!validLayerArtifactRef(witness.DetectedRoot)||witness.CurrentLines==witness.DetectedRoot||!validLayerArtifactRef(producer.Params){return fail()}
 if _,err=domain.ParseDigest(producer.InputDigest);err!=nil{return fail()}
 profileWire,err:=canonical.Bytes([]byte(producer.ProfileJCS));if err!=nil||!bytes.Equal(profileWire,[]byte(producer.ProfileJCS)){return fail()}
 var profile map[string]json.RawMessage;if json.Unmarshal(profileWire,&profile)!=nil||len(profile)==0{return fail()}
 digest:=sha256.Sum256([]byte(producer.ProfileJCS));if hex.EncodeToString(digest[:])!=producer.ProfileSHA{return fail()}
 detected,err:=v2optical.ParseDetectedPerspectiveLines(detectedRaw,witness.DetectedRoot,current.Base.AssetVersionID.String())
 if err!=nil||detected.Base!=current.Base||detected.Status!="available"||detected.Lines==nil{return fail()}
 exact,sha,err:=detected.Lines.Compile();if err!=nil||!bytes.Equal(exact,segmentsRaw)||sha!=witness.CurrentLines.SHA256{return fail()}
 current.Lines=&witness.CurrentLines
 body,sha,err:=current.Compile();if err!=nil{return fail()}
 selected.Params.Body=body;selected.Params.SHA256=sha
 return selected,nil
}
