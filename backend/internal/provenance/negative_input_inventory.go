package provenance

import (
 "bytes"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// Reconstruct the normative negative inventory from physically verified source
// metadata and qualification. No arbitrary or unaccounted input is accepted.
func CompileSigningNegativeInputInventory(task CanonicalSigningTask,evidence SigningNegativeEvidence)([]v2input.Entry,error){
 if _,err:=compileSigningNegativeEvidenceStamp(task,evidence);err!=nil{return nil,err}
 source,err:=canonical.Marshal(evidence.pixels.source);if err!=nil{return nil,err}
 arrays,_,err:=v2optical.NegativeImageInputs(evidence.pixels.params,source,evidence.qualification.Report);if err!=nil{return nil,err}
 entries:=[]v2input.Entry{{Ref:task.Params,Contract:"Params"},{Ref:evidence.pixels.params.Base.Source,Contract:"WorkingImage"},{Ref:evidence.pixels.params.Calibration,Contract:"NegativeCalibration"},{Ref:evidence.qualification.Report,Contract:"QualificationReport"}}
 entries=append(entries,arrays...)
 sort.Slice(entries,func(i,j int)bool{return bytes.Compare(entries[i].Ref.ArtifactID[:],entries[j].Ref.ArtifactID[:])<0})
 if err:=verifySigningNegativeInputDigest(task,entries);err!=nil{return nil,err}
 return entries,nil
}

func verifySigningNegativeInputDigest(task CanonicalSigningTask,entries []v2input.Entry)error{
 if task.Task==uuid.Nil||task.Operation!="optical.negative"||task.Version!="1"||len(entries)<5||len(entries)>134{return ErrInvalid}
 counts:=map[string]int{}
 for i,entry:=range entries{
  if !ref(entry.Ref)||(i>0&&bytes.Compare(entries[i-1].Ref.ArtifactID[:],entry.Ref.ArtifactID[:])>=0){return ErrInvalid}
  switch entry.Contract{case "Params","WorkingImage","NegativeCalibration","QualificationReport","NpyArray":default:return ErrInvalid}
  if entry.Contract=="Params"&&entry.Ref!=task.Params{return ErrInvalid}
  counts[entry.Contract]++
 }
 for _,contract:=range []string{"Params","WorkingImage","NegativeCalibration","QualificationReport"}{if counts[contract]!=1{return ErrInvalid}}
 if counts["NpyArray"]<1{return ErrInvalid}
 sha,err:=canonical.Hash(struct{Task uuid.UUID `json:"task"`;Operation string `json:"operation"`;Version string `json:"version"`;Entries []v2input.Entry `json:"entries"`}{task.Task,task.Operation,task.Version,entries})
 if err!=nil||sha!=task.InputDigest{return ErrInvalid};return nil
}

