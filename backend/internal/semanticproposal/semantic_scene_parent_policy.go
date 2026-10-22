package semanticproposal

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/v2input"
)

// The original detector's immutable recovery metadata selects whether this
// accepted parent needs scene prerequisites. A current process configuration
// cannot silently remove or substitute that requirement after restart.
func frozenSemanticSceneParentPolicy(ctx context.Context,parent detectorDraftPlan)(*TransferScenePreparation,error){
 if err:=verifyDetectorPlanSeal(ctx,parent);err!=nil{return nil,err}
 var body struct{Stage struct{Recovery json.RawMessage `json:"recovery"`} `json:"v27_detector_prepare"`}
 if len(parent.shell.Tasks)!=1 || json.Unmarshal(parent.shell.Config,&body)!=nil{return nil,v2input.ErrInvalid}
 metadata,err:=decodeNativePlanRecoveryMetadata(body.Stage.Recovery,parent.shell.Tasks[0])
 if err!=nil{return nil,err}
 if metadata.ScenePolicy==nil{return nil,nil}
 return metadata.ScenePolicy.preparation()
}
