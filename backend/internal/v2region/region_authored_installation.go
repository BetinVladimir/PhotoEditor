package v2region

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "os"
 "path/filepath"
 "syscall"
 "photoeditor/backend/internal/canonical"
)

// The API consumes the exact operator-installed presentation inventory. Native
// verification still occurs in the worker and independent adopter. This is not
// a renderer qualification, operation enablement or proof that libraries exist
// on the API host.
type AuthoredRegionInstallation struct {RuntimeSHA,ICCLockSHA string}
func LoadAuthoredRegionInstallation(path,expectedSHA,iccLockSHA string)(AuthoredRegionInstallation,error){
 var empty AuthoredRegionInstallation
 if !filepath.IsAbs(path)||!validRegionSHA(expectedSHA)||!validRegionSHA(iccLockSHA){return empty,ErrRegionInvalid}
 file,err:=os.OpenFile(path,os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK,0);if err!=nil{return empty,ErrRegionUnavailable};defer file.Close()
 before,err:=file.Stat();if err!=nil||!before.Mode().IsRegular()||before.Size()<2||before.Size()>65536{return empty,ErrRegionInvalid}
 native,ok:=before.Sys().(*syscall.Stat_t);if !ok||native.Nlink!=1{return empty,ErrRegionInvalid}
 raw,err:=io.ReadAll(io.LimitReader(file,65537));if err!=nil{return empty,ErrRegionUnavailable}
 after,err:=file.Stat();if err!=nil{return empty,ErrRegionUnavailable};linked,err:=os.Lstat(path)
 if err!=nil||!os.SameFile(before,after)||!os.SameFile(before,linked)||before.Size()!=after.Size()||before.Size()!=int64(len(raw))||!before.ModTime().Equal(after.ModTime())||!before.ModTime().Equal(linked.ModTime()){return empty,ErrRegionInvalid}
 sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=expectedSHA{return empty,ErrRegionInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return empty,ErrRegionInvalid}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil||len(fields)!=6{return empty,ErrRegionInvalid}
 for _,name:=range []string{"schema_version","operation","version","backend","icc_lock_sha256","components"}{if _,ok:=fields[name];!ok{return empty,ErrRegionInvalid}}
 value:=func(key string)string{var out string;_ = json.Unmarshal(fields[key],&out);return out}
 if !bytes.Equal(fields["schema_version"],[]byte("1"))||value("operation")!="region.authored_crop"||value("version")!="1"||value("backend")!="cpu"||value("icc_lock_sha256")!=iccLockSHA{return empty,ErrRegionInvalid}
 var components map[string]string;if json.Unmarshal(fields["components"],&components)!=nil||len(components)!=21{return empty,ErrRegionInvalid}
 for _,name:=range []string{"compute","inputs","crop","installation","replay","export_resize","result","resources","presentation","projection","pixels","icc","codec","arrays","numpy_native","pillow_native","pillow_lcms_native","python","lcms_native","opencv_native","vips_native"}{if !validRegionSHA(components[name]){return empty,ErrRegionInvalid}}
 for name,pin:=range components{if name==""||!validRegionSHA(pin){return empty,ErrRegionInvalid}}
 return AuthoredRegionInstallation{RuntimeSHA:expectedSHA,ICCLockSHA:iccLockSHA},nil
}
func(i AuthoredRegionInstallation)Profile()([]byte,error){
 if !validRegionSHA(i.RuntimeSHA)||!validRegionSHA(i.ICCLockSHA){return nil,ErrRegionInvalid}
 return canonical.Marshal(map[string]string{"backend":"cpu","renderer_version":"v2.2","runtime_lock_sha":i.RuntimeSHA,"icc_lock_sha":i.ICCLockSHA})
}
