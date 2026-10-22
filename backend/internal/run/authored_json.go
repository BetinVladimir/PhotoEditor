package run
import("bytes";"encoding/json";"io";"photoeditor/backend/internal/v2input")
func decodeAuthoredJSON(raw []byte,target any)error{
 if len(raw)<2||len(raw)>4<<20{return v2input.ErrInvalid}
 d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if err:=d.Decode(target);err!=nil{return err}
 var tail any;if d.Decode(&tail)!=io.EOF{return v2input.ErrInvalid};return nil
}
