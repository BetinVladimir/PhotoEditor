package semanticproposal

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

const semanticSceneCheck = "semantic.scene.farneback_depth_plane_v1"

// This comparable private proof binds deployment policy bytes to the physically
// verified qualification and current authorized object versions. It is not an
// admission lock; transaction admission must repeat the qualification check.
type semanticScenePolicyProof struct {
 qualification transferQualificationPreparationProof
 policy string
 sha string
}

func scenePolicyFromVerifiedReport(raw []byte, expected v2qualification.Expected, report v2qualification.Report) (string,string,error) {
 policy,sha,err:=v2input.DecodeSemanticScenePolicy(raw)
 if err!=nil {return "","",err}
 if _,ok:=expected.EvidenceSHAs[sha];!ok {return "","",v2input.ErrInvalid}
 required:=false
 for _,check:=range expected.RequiredChecks {if check==semanticSceneCheck {required=true}}
 if !required {return "","",v2input.ErrInvalid}
 found:=false
 for _,check:=range report.Checks {
  if check.ID==semanticSceneCheck {
   if found || !check.Passed || check.EvidenceSHA!=sha {return "","",v2input.ErrInvalid}
   found=true
  }
 }
 if !found {return "","",v2input.ErrInvalid}
 body,err:=canonical.Marshal(policy)
 if err!=nil {return "","",err}
 return string(body),sha,nil
}

func (s SQLTransferSourceReader) readSemanticScenePolicy(ctx context.Context,who auth.Identity,tenant uuid.UUID,pin v2input.SemanticQualificationPin,expected v2qualification.Expected,raw json.RawMessage)(semanticScenePolicyProof,error){
 var zero semanticScenePolicyProof
 if err:=ctx.Err();err!=nil {return zero,err}
 // Detach deployment values before any catalog or object await.
 expected=ownQualificationExpected(expected)
 raw=append(json.RawMessage(nil),raw...)
 if _,_,err:=v2input.DecodeSemanticScenePolicy(raw);err!=nil {return zero,err}
 q,err:=s.readTransferQualificationPreparation(ctx,who,tenant,pin,expected)
 if err!=nil {return zero,err}
 reportBody,err:=v2input.ReadPinnedSemanticDocument(ctx,s.Objects,q.objects[1].object,pin.Report)
 if err!=nil {return zero,err}
 report,err:=v2qualification.Verify(reportBody,pin.Report.SHA256,expected)
 if err!=nil {return zero,err}
 policy,sha,err:=scenePolicyFromVerifiedReport(raw,expected,report)
 if err!=nil {return zero,err}
 current,err:=s.qualificationObjects(ctx,who,tenant,pin)
 if err!=nil {return zero,err}
 if len(current)!=2 || current[0]!=q.objects[0] || current[1]!=q.objects[1] {return zero,v2input.ErrConflict}
 if err:=ctx.Err();err!=nil {return zero,err}
 return semanticScenePolicyProof{qualification:q,policy:policy,sha:sha},nil
}
