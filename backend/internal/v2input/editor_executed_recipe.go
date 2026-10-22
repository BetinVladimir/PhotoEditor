package v2input

import (
 "bytes"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
)

// ValidateExecutedEditingRecipeV3 is a shape check for the private final
// composition path. It does not grant receipt authority. Public draft/session
// admission continues to use ValidateEditingRecipeV3 and rejects this field.
func ValidateExecutedEditingRecipeV3(raw []byte,binding SessionVersionBinding)error{
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return ErrInvalid}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return ErrInvalid}
 receiptRaw,ok:=fields["executed_base_receipt"];if !ok||!exactJSONKeys(receiptRaw,"artifact_id","sha256"){return ErrInvalid}
 var receipt Ref;if json.Unmarshal(receiptRaw,&receipt)!=nil||!validRef(receipt){return ErrInvalid}
 delete(fields,"executed_base_receipt");base,err:=canonical.Marshal(fields);if err!=nil||validateSessionRecipe(base,binding)!=nil{return ErrInvalid}
 var recipe sessionRecipe;if json.Unmarshal(base,&recipe)!=nil||recipe.SessionCorrections!=nil||recipe.LearningVersionID!=nil||recipe.LayerStack!=nil||recipe.ProtectedMask!=nil||len(recipe.Overrides)!=0||recipe.ParameterDelta!=(sessionCorrectionVector{}){return ErrInvalid}
 return nil
}
