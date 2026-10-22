package style

import (
 "bytes"
 "encoding/json"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

func readCreatedStyleReceipt(response idempotency.Response, tid uuid.UUID, req Create)(Style,error){
 var item Style
 if response.Status!=201||response.ResourceID==nil||*response.ResourceID==uuid.Nil||json.Unmarshal(response.Body,&item)!=nil||item.ID!=*response.ResourceID||item.TenantID!=tid||item.ProjectID!=req.ProjectID||item.Name!=strings.TrimSpace(req.Name)||item.Description!=req.Description||item.Revision!="1"||item.Archived||item.CreatedAt.IsZero(){return Style{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 expected,e:=canonical.Marshal(item);if e!=nil{return Style{},e};actual,e:=canonical.Bytes(response.Body);if e!=nil{return Style{},e}
 if !bytes.Equal(expected,actual){return Style{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 return item,nil
}
