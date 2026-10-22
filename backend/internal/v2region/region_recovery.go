package v2region

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

func regionRequestSHA(tenant,asset uuid.UUID,request RegionPreviewRequest)(string,error){
 return canonical.Hash(struct{Tenant uuid.UUID `json:"tenant_id"`;Asset uuid.UUID `json:"asset_id"`;Request RegionPreviewRequest `json:"request"`}{tenant,asset,request})
}
func readRegionRecovery(raw []byte,tenant,asset,run,workflow uuid.UUID,request RegionPreviewRequest)(v2input.JobTicket,error){
 var zero v2input.JobTicket
 var receipt struct{Ticket v2input.JobTicket `json:"ticket"`;Config json.RawMessage `json:"config"`;ConfigHash string `json:"config_hash"`}
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&receipt)!=nil||decoder.Decode(new(any))!=io.EOF||receipt.Ticket.RunID!=run||receipt.Ticket.WorkflowID!=workflow||receipt.Ticket.State!="queued"{return zero,ErrRegionInvalid}
 hash,err:=canonical.Hash(receipt.Config);if err!=nil||hash!=receipt.ConfigHash{return zero,ErrRegionInvalid}
 var config struct{Region struct{Nodes []regionNodeDocument `json:"nodes"`} `json:"v26_region"`}
 if json.Unmarshal(receipt.Config,&config)!=nil||len(config.Region.Nodes)!=1{return zero,ErrRegionInvalid}
 var params regionPlanDocument;if json.Unmarshal([]byte(config.Region.Nodes[0].ParamsJCS),&params)!=nil||params.TenantID!=tenant||params.AssetID!=asset{return zero,ErrRegionInvalid}
 a,err:=canonical.Marshal(request);if err!=nil{return zero,err};b,err:=canonical.Marshal(params.Request);if err!=nil||!bytes.Equal(a,b){return zero,ErrRegionInvalid}
 return receipt.Ticket,nil
}
func(s QueueService)acceptedRegion(ctx context.Context,who auth.Identity,tenant,asset,run,workflow,task uuid.UUID,key,requestSHA string,request RegionPreviewRequest)(v2input.JobTicket,bool,error){
 var ticket v2input.JobTicket;found:=false
 err:=database.TenantTransaction(ctx,s.DB,tenant,func(tx pgx.Tx)error{
  var raw []byte;err:=tx.QueryRow(ctx,`SELECT photo.v26_region_accepted_recovery($1,$2,$3,$4,$5,$6,$7,$8,$9)`,tenant,asset,who.UserID,who.SessionID,key,requestSHA,run,workflow,task).Scan(&raw)
  if err!=nil{var pg *pgconn.PgError;if errors.As(err,&pg){if pg.Code=="23505"{return v2input.ErrConflict};if pg.Code=="23514"||pg.Code=="22023"{return ErrRegionInvalid}};return err}
  if len(raw)==0{return nil};ticket,err=readRegionRecovery(raw,tenant,asset,run,workflow,request);found=err==nil;return err
 });if ctx.Err()!=nil{return v2input.JobTicket{},false,ctx.Err()};return ticket,found,err
}
