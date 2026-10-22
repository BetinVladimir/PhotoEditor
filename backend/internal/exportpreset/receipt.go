package exportpreset

import (
 "bytes"
 "encoding/json"
 "errors"
 "strconv"
 "strings"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
)

func readPresetReceipt(response idempotency.Response,status int)(Preset,error){
 var value Preset
 invalid:=func()(Preset,error){return Preset{},errors.New("invalid export preset receipt")}
 if response.Status!=status||response.ResourceID==nil||*response.ResourceID==uuid.Nil||json.Unmarshal(response.Body,&value)!=nil||value.ID!=*response.ResourceID||value.CreatedAt.IsZero()||strings.TrimSpace(value.Name)==""||len([]rune(value.Name))>120||ValidateConfig(value.Config)!=nil{return invalid()}
 revision,err:=strconv.ParseInt(value.Revision,10,64)
 if err!=nil||revision<1||strconv.FormatInt(revision,10)!=value.Revision{return invalid()}
 hash,err:=canonical.Hash(value.Config);if err!=nil||hash!=value.ConfigHash{return invalid()}
 actual,err:=canonical.Marshal(value);if err!=nil{return invalid()}
 stored,err:=canonical.Bytes(response.Body);if err!=nil||!bytes.Equal(actual,stored){return invalid()}
 return value,nil
}
func validateCreatedPresetReceipt(response idempotency.Response,request Create)error{
 value,err:=readPresetReceipt(response,201);if err!=nil{return err}
 want,err:=canonical.Marshal(request.Config);if err!=nil{return err};actual,err:=canonical.Marshal(value.Config);if err!=nil{return err}
 if value.Revision!="1"||value.Name!=strings.TrimSpace(request.Name)||!bytes.Equal(want,actual){return errors.New("export preset create receipt binding mismatch")}
 return nil
}
