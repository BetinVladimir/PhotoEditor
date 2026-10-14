package interchange

import (
 "encoding/json"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
// Internal server-to-SQL evidence; never accepted from the public decision body.
type ImportStyleApprovalReceipt struct{
 Parsed string `json:"parsed"`
 Recipe string `json:"recipe"`
 Binding v2input.SessionVersionBinding `json:"binding"`
 Style v2input.Ref `json:"style"`
 Config string `json:"config"`
}
func validateImportStyleApprovalReceipt(workflow,entry uuid.UUID,receipt ImportStyleApprovalReceipt)error{
 if workflow==uuid.Nil||entry==uuid.Nil||receipt.Binding.AssetVersionID==uuid.Nil||!validImportRef(receipt.Binding.Source)||!validImportRef(v2input.Ref{ArtifactID:entry,SHA256:receipt.Binding.CanvasHash})||!validImportRef(receipt.Style)||len(receipt.Parsed)<2||len(receipt.Parsed)>8<<20||len(receipt.Config)<2||len(receipt.Config)>8<<20||rawSHA([]byte(receipt.Config))!=receipt.Style.SHA256{return v2input.ErrInvalid}
 if err:=validateImportStyleRecipe(receipt);err!=nil{return err}
 if _,err:=validateParsedImportDocument(context.Background(),[]byte(receipt.Parsed));err!=nil{return err}
 var parsed importParsedDocument;if err:=decodeImportDocument([]byte(receipt.Parsed),&parsed);err!=nil{return err}
 matches:=0
 for _,item:=range parsed.Manifest.Inventory{if item.Contract=="StyleV2"&&uuid.NewSHA1(workflow,[]byte("interchange:dependency:"+entry.String()+":StyleV2:"+item.ID.String()))==receipt.Style.ArtifactID{matches++}}
 if matches!=1{return v2input.ErrInvalid}
 if _,err:=v2draft.DecodeStyleBaseline([]byte(receipt.Config));err!=nil{return err}
 var fields map[string]json.RawMessage
 if err:=decodeImportDocument([]byte(receipt.Config),&fields);err!=nil{return err}
 return nil
}

// The receipt carries the exact authored document, not a reconstructed recipe.
// SQL independently binds its SHA to the locked current draft before approval.
func validateImportStyleRecipe(receipt ImportStyleApprovalReceipt)error{
 raw:=[]byte(receipt.Recipe)
 if len(raw)<2||len(raw)>8<<20{return v2input.ErrInvalid}
 if err:=v2input.ValidateEditingRecipeV3(raw,receipt.Binding);err!=nil{return err}
 var recipe struct{BaseStyle v2input.Ref `json:"base_style"`}
 if err:=json.Unmarshal(raw,&recipe);err!=nil{return err}
 if recipe.BaseStyle!=receipt.Style{return v2input.ErrInvalid}
 return nil
}
