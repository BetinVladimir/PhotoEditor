package v2region

import (
 "crypto/sha256"
 "encoding/hex"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "photoeditor/backend/internal/canonical"
)

func TestAuthoredPresentationLockBindsCanonicalInventoryAndICC(t *testing.T){
 components:=map[string]string{}
 for _,key:=range []string{"compute","inputs","crop","installation","replay","export_resize","result","resources","presentation","projection","pixels","icc","codec","arrays","numpy_native","pillow_native","pillow_lcms_native","python","lcms_native","opencv_native","vips_native"}{components[key]=strings.Repeat("a",64)}
 icc:=strings.Repeat("b",64)
 lock:=map[string]any{"schema_version":1,"operation":"region.authored_crop","version":"1","backend":"cpu","icc_lock_sha256":icc,"components":components}
 raw,e:=canonical.Marshal(lock);if e!=nil{t.Fatal(e)};sum:=sha256.Sum256(raw);pin:=hex.EncodeToString(sum[:])
 path:=filepath.Join(t.TempDir(),"runtime.json");if e=os.WriteFile(path,raw,0600);e!=nil{t.Fatal(e)}
 good,e:=LoadAuthoredRegionInstallation(path,pin,icc);if e!=nil||good.RuntimeSHA!=pin||good.ICCLockSHA!=icc{t.Fatalf("valid inventory: %v",e)}
 if _,e=LoadAuthoredRegionInstallation(path,pin,strings.Repeat("c",64));e!=ErrRegionInvalid{t.Fatalf("foreign ICC accepted: %v",e)}
 link:=path+".symlink";if e=os.Symlink(path,link);e!=nil{t.Fatal(e)};if _,e=LoadAuthoredRegionInstallation(link,pin,icc);e==nil{t.Fatal("symlink accepted")}
 if e=os.WriteFile(path,append(raw,' '),0600);e!=nil{t.Fatal(e)};changed:=sha256.Sum256(append(raw,' '));if _,e=LoadAuthoredRegionInstallation(path,hex.EncodeToString(changed[:]),icc);e!=ErrRegionInvalid{t.Fatalf("noncanonical bytes accepted: %v",e)}
 if e=os.WriteFile(path,raw,0600);e!=nil{t.Fatal(e)};hard:=path+".hardlink";if e=os.Link(path,hard);e!=nil{t.Fatal(e)};if _,e=LoadAuthoredRegionInstallation(path,pin,icc);e!=ErrRegionInvalid{t.Fatalf("hardlink accepted: %v",e)}
}
