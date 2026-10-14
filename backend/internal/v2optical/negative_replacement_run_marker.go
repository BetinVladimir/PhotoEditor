package v2optical

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type negativeReplacementRunMarker struct{
 Version string `json:"version"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 Hash string `json:"decision_hash"`
 OriginalRun uuid.UUID `json:"original_run_id"`
 OriginalTask uuid.UUID `json:"original_task_id"`
}

// RunMarker returns canonical owned bytes for config_snapshot's exact membership
// marker and its digest. It is not a draft, admission token or queue receipt.
func(p PreparedMappedReplacementStep)RunMarker(ctx context.Context)([]byte,string,error){
 if err:=ctx.Err();err!=nil{return nil,"",err}
 step:=p.parents.step;l:=step.intent.locator;n:=step.node
 if l.Tenant==uuid.Nil||l.Project==uuid.Nil||l.Review==uuid.Nil||l.Task==uuid.Nil||l.Run==uuid.Nil||step.intent.who.UserID==uuid.Nil||step.intent.who.SessionID==uuid.Nil||n.OriginalTask!=l.Task||n.OriginalRun!=l.Run{return nil,"",v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(l.Hash);err!=nil{return nil,"",v2input.ErrInvalid}
 sum:=sha256.Sum256([]byte(step.intent.raw));if hex.EncodeToString(sum[:])!=l.Params.SHA256||int64(len(step.intent.raw))!=l.Params.Bytes{return nil,"",v2input.ErrInvalid}
 if _,err:=opticalFrozenBindings(l.Operation,[]byte(step.intent.raw));err!=nil{return nil,"",err}
 namespace:=uuid.NewSHA1(l.Tenant,[]byte("V29.NegativeReplacement/v2:"+l.Project.String()+":"+l.Review.String()+":"+l.Hash))
 if n.Task!=uuid.NewSHA1(namespace,[]byte("task:"+l.Task.String()))||n.Run!=uuid.NewSHA1(namespace,[]byte("run:"+l.Run.String()+":"+l.Task.String())){return nil,"",v2input.ErrInvalid}
 marker:=negativeReplacementRunMarker{Version:"v29_negative_replacement_v2",Project:l.Project,Review:l.Review,Hash:l.Hash,OriginalRun:l.Run,OriginalTask:l.Task}
 raw,err:=canonical.Marshal(marker);if err!=nil{return nil,"",err};digest,err:=canonical.Hash(marker);if err!=nil{return nil,"",err}
 return raw,digest,ctx.Err()
}
