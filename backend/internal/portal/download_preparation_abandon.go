package portal

import (
 "context"
 "bytes"
 "encoding/json"
 "io"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

type DownloadPreparationAbandonRequest struct {
 SelectionKey string `json:"selection_key"`
 StartKey string `json:"start_key"`
 Selection SelectionRequest `json:"selection"`
 PresetID uuid.UUID `json:"preset_id"`
 SnapshotID *uuid.UUID `json:"snapshot_id"`
}
type DownloadPreparationResolution struct {
 Status string `json:"status"`
 SelectionKey string `json:"selection_key"`
 StartKey string `json:"start_key"`
 PresetID uuid.UUID `json:"preset_id"`
 CollectionID *uuid.UUID `json:"collection_id,omitempty"`
 Ticket *DownloadAccepted `json:"ticket,omitempty"`
}
func(s Service) AbandonDownloadPreparation(ctx context.Context,token,csrf string,raw DownloadPreparationAbandonRequest)(DownloadPreparationResolution,error){
 var out DownloadPreparationResolution
 if s.DB==nil||len(s.Pepper)<32{return out,errors.New("portal dependencies unavailable")}
 if !idempotency.ValidKey(raw.SelectionKey)||!idempotency.ValidKey(raw.StartKey)||raw.PresetID==uuid.Nil||raw.SnapshotID!=nil&&*raw.SnapshotID==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 selection,_,_,err:=normalizeSelection(raw.Selection);if err!=nil{return out,err}
 hash,err:=canonical.Hash(selection);if err!=nil{return out,err}
 var outcome string;var body []byte
 err=s.DB.QueryRow(ctx,`SELECT outcome,receipt FROM photo.abandon_portal_download_preparation($1,$2,$3,$4,$5,$6,$7)`,auth.MAC(s.Pepper,"portal-session",token),auth.MAC(s.Pepper,"portal-csrf",token,csrf),raw.SelectionKey,hash,raw.StartKey,raw.SnapshotID,raw.PresetID).Scan(&outcome,&body)
 if err!=nil{return out,err};if err=ctx.Err();err!=nil{return out,err}
 switch outcome {
 case "accepted":
  var accepted DownloadAccepted
  if err=decodePortalPreparationReceipt(body,&accepted);err!=nil||accepted.DownloadJobID==uuid.Nil||accepted.Status!="queued"{return out,errors.New("invalid accepted preparation receipt")}
  return DownloadPreparationResolution{Status:"accepted",SelectionKey:raw.SelectionKey,StartKey:raw.StartKey,PresetID:raw.PresetID,Ticket:&accepted},nil
 case "abandoned":
  if err=decodePortalPreparationReceipt(body,&out);err!=nil||out.Status!="abandoned"||out.SelectionKey!=raw.SelectionKey||out.StartKey!=raw.StartKey||out.PresetID!=raw.PresetID||(out.CollectionID==nil||*out.CollectionID==uuid.Nil)||out.Ticket!=nil{return DownloadPreparationResolution{},errors.New("invalid abandoned preparation receipt")};return out,nil
 case "unauthorized":return out,domain.NewError("INVALID_GUEST_SESSION",401)
 case "conflict":return out,domain.NewError("IDEMPOTENCY_CONFLICT",409)
 default:return out,domain.NewError("VALIDATION_FAILED",422)
 }
}

func decodePortalPreparationReceipt(body []byte,target any)error{
 if len(body)<2||len(body)>4096{return errors.New("invalid preparation receipt size")}
 decoder:=json.NewDecoder(bytes.NewReader(body));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(target);err!=nil{return err};if err:=decoder.Decode(new(any));err!=io.EOF{return errors.New("invalid preparation receipt trailing bytes")};return nil
}
