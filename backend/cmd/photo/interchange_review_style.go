package main

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "time"

 "photoeditor/backend/internal/interchange"
)

type interchangeReviewStyleConfig struct {
 Schema string `json:"schema_version"`
 Python string `json:"python_path"`
 WorkerRoot string `json:"worker_root"`
 ScratchRoot string `json:"scratch_root"`
 ScratchBytes int64 `json:"scratch_bytes"`
 TimeoutSeconds int `json:"timeout_seconds"`
 RuntimeSHA string `json:"runtime_sha256"`
 Models map[string]string `json:"models"`
}

// Deployment supplies independently qualified runtime/model pins. Startup never
// infers qualification from the interpreter or grants database privileges.
func buildInterchangeReviewStyle(icc string)(interchange.ImportReviewStyleQualifierFactory,error){
 path,pin:=os.Getenv("PHOTO_V30_IMPORT_STYLE_CONFIG"),os.Getenv("PHOTO_V30_IMPORT_STYLE_CONFIG_SHA256")
 if path==""&&pin=="" {return nil,nil}
 invalid:=errors.New("interchange review style configuration invalid")
 if !filepath.IsAbs(path)||len(pin)!=64 {return nil,invalid}
 expected,err:=hex.DecodeString(pin);if err!=nil||hex.EncodeToString(expected)!=pin{return nil,invalid}
 file,err:=os.Open(path);if err!=nil{return nil,invalid};defer file.Close()
 info,err:=file.Stat();if err!=nil||!info.Mode().IsRegular()||info.Size()<2||info.Size()>1<<20{return nil,invalid}
 raw,err:=io.ReadAll(io.LimitReader(file,(1<<20)+1));if err!=nil||len(raw)>1<<20{return nil,invalid}
 digest:=sha256.Sum256(raw);if !bytes.Equal(digest[:],expected){return nil,invalid}
 var cfg interchangeReviewStyleConfig
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if decoder.Decode(&cfg)!=nil||decoder.Decode(new(any))!=io.EOF{return nil,invalid}
 if cfg.Schema!="interchange-review-style-config/v1"||!filepath.IsAbs(cfg.Python)||!filepath.IsAbs(cfg.WorkerRoot)||cfg.TimeoutSeconds<1||cfg.TimeoutSeconds>600{return nil,invalid}
 pythonInfo,err:=os.Stat(cfg.Python);if err!=nil||!pythonInfo.Mode().IsRegular()||pythonInfo.Mode().Perm()&0111==0{return nil,invalid}
 workerInfo,err:=os.Stat(cfg.WorkerRoot);if err!=nil||!workerInfo.IsDir(){return nil,invalid}
 scratch,err:=interchange.NewExportArchiveScratch(cfg.ScratchRoot,cfg.ScratchBytes);if err!=nil{return nil,invalid}
 probe:=interchange.PythonExportStyleProbe{Python:cfg.Python,WorkerRoot:cfg.WorkerRoot,Timeout:time.Duration(cfg.TimeoutSeconds)*time.Second}
 factory,err:=interchange.NewNativeImportReviewStyleFactory(scratch,probe,cfg.Models,cfg.RuntimeSHA,icc)
 if err!=nil{return nil,invalid};return factory,nil
}
