package v2portrait

import (
 "bytes"
 "context"
 "encoding/json"
 "math"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// GlareSourceParams retains the actual generative contract. A donor binding
// cannot be manufactured merely to reuse the donor confirmation publisher.
type GlareSourceParams struct {
 SchemaVersion int `json:"schema_version"`
 Base v2input.SessionVersionBinding `json:"base"`
 Strength float64 `json:"strength"`
 Donor *v2input.SessionVersionBinding `json:"donor"`
 GlassesMask v2input.Ref `json:"glasses_mask"`
 Protected v2input.Ref `json:"protected"`
 Mode string `json:"mode"`
 Confirmation v2input.Ref `json:"confirmation"`
 ModelLockSHA string `json:"model_lock_sha"`
 Calibration v2input.Ref `json:"calibration"`
 SourceFaces v2input.Ref `json:"source_faces"`
 DonorFaces *v2input.Ref `json:"donor_faces"`
}

func ParseGlareSourceParams(raw []byte)(GlareSourceParams,error){
 var p GlareSourceParams
 if len(raw)<2||len(raw)>65536||json.Unmarshal(raw,&p)!=nil{return p,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(p)
 if err!=nil||!bytes.Equal(exact,raw)||p.SchemaVersion!=1||p.Mode!="generative"||p.Donor!=nil||p.DonorFaces!=nil||!validDonorBinding(p.Base)||math.IsNaN(p.Strength)||math.IsInf(p.Strength,0)||p.Strength<0||p.Strength>1{return GlareSourceParams{},v2input.ErrInvalid}
 if _,err=domain.ParseDigest(p.ModelLockSHA);err!=nil{return GlareSourceParams{},v2input.ErrInvalid}
 seen:=map[uuid.UUID]bool{p.Base.Source.ArtifactID:true}
 for _,ref:=range []v2input.Ref{p.GlassesMask,p.Protected,p.Confirmation,p.Calibration,p.SourceFaces}{
  if ref.ArtifactID==uuid.Nil||seen[ref.ArtifactID]{return GlareSourceParams{},v2input.ErrInvalid};seen[ref.ArtifactID]=true
  if _,err=domain.ParseDigest(ref.SHA256);err!=nil{return GlareSourceParams{},v2input.ErrInvalid}
 }
 return p,nil
}

// BuildGlareSourceConfirmation is a transaction-scoped proof builder only.
// Publication must repeat this authority at reserve and completion; this
// returned document alone is never an admission ticket.
func BuildGlareSourceConfirmation(ctx context.Context,tx pgx.Tx,who auth.Identity,
 tenant,project,shoot uuid.UUID,paramsRaw []byte,observation uuid.UUID)([]byte,string,error){
 if err:=ctx.Err();err!=nil{return nil,"",err}
 p,err:=ParseGlareSourceParams(paramsRaw);if err!=nil{return nil,"",err}
 if tx==nil||tenant==uuid.Nil||project==uuid.Nil||shoot==uuid.Nil||observation==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return nil,"",v2input.ErrInvalid}
 var role string
 if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return nil,"",err}
 if role!="photo_scheduler"{return nil,"",v2input.ErrInvalid}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.build_v28_glare_source_confirmation($1,$2,$3,$4,$5,$6::jsonb,$7)",tenant,project,shoot,who.UserID,who.SessionID,string(paramsRaw),observation).Scan(&raw);err!=nil{return nil,"",err}
 if len(raw)<2||len(raw)>65536{return nil,"",v2input.ErrExpired}
 var proof DonorConfirmation
 if json.Unmarshal(raw,&proof)!=nil{return nil,"",v2input.ErrInvalid}
 body,sha,err:=proof.Compile();normal,normalErr:=canonical.Bytes(raw)
 if err!=nil||normalErr!=nil||!bytes.Equal(body,normal)||proof.Source!=p.Base||proof.Donor!=nil||proof.DonorFaceID!=nil||proof.DonorObservationID!=nil||proof.ActorID!=who.UserID||proof.SourceObservationID!=observation{return nil,"",v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return nil,"",err}
 return body,sha,nil
}
