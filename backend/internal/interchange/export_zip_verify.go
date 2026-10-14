package interchange

import (
 "archive/zip"
 "context"
 "crypto/sha256"
 "encoding/binary"
 "encoding/hex"
 "io"
 "strings"
 "golang.org/x/text/cases"
 "photoeditor/backend/internal/v2input"
)

type VerifiedExportZIPFile struct{Path string;Bytes int64;SHA256 string}
// VerifyExportZIPInventory verifies bounded ZIP/ZIP64 metadata and every member
// through CRC-checked decompression. It never extracts user-controlled paths.
// Manifest/source binding and physical image qualification are separate gates.
func VerifyExportZIPInventory(ctx context.Context,source io.ReaderAt,size int64,limits ExportLimits)([]VerifiedExportZIPFile,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if source==nil||limits.ArchiveBytes<1||limits.ArchiveBytes>64<<30||limits.ExpandedBytes<1||limits.ExpandedBytes>128<<30||limits.Files<1||limits.Files>30000||size<22||size>limits.ArchiveBytes{return nil,v2input.ErrInvalid}
 if err:=exportZIPMetadataBudget(source,size,limits.Files);err!=nil{return nil,err}
 archive,err:=zip.NewReader(source,size);if err!=nil{return nil,err}
 if len(archive.File)<1||len(archive.File)>limits.Files||archive.Comment!=""{return nil,v2input.ErrInvalid}
 seen:=map[string]bool{};previous:="";expanded:=int64(0);manifest:=false
 files:=make([]VerifiedExportZIPFile,0,len(archive.File));buffer:=make([]byte,256<<10)
 for _,file:=range archive.File{
  if err:=ctx.Err();err!=nil{return nil,err}
  folded:=cases.Fold().String(file.Name)
  if !validImportPlanPath(file.Name)||seen[folded]||file.Name<=previous||!file.Mode().IsRegular()||file.Flags&1!=0||file.Comment!=""||file.ModifiedDate!=33||file.ModifiedTime!=0||file.NonUTF8||file.UncompressedSize64>uint64(limits.ExpandedBytes-expanded)||file.CompressedSize64>uint64(size){return nil,v2input.ErrInvalid}
  if file.Method!=zip.Store&&file.Method!=zip.Deflate{return nil,v2input.ErrInvalid}
  // The writer uses stored encoding for images/binary arrays and deflate for
  // JSON/XMP. Deflate level cannot be recovered reliably from a ZIP stream.
  sidecar:=strings.HasSuffix(file.Name,".json")||strings.HasSuffix(file.Name,".xmp")
  if (!sidecar&&file.Method!=zip.Store)||(sidecar&&file.Method!=zip.Deflate){return nil,v2input.ErrInvalid}
  if file.UncompressedSize64>100*max(file.CompressedSize64,uint64(1)){return nil,v2input.ErrInvalid}
  seen[folded]=true;previous=file.Name;manifest=manifest||file.Name=="manifest.json"
  stream,err:=file.Open();if err!=nil{return nil,err}
  digest:=sha256.New()
  count,readErr:=io.CopyBuffer(digest,io.LimitReader(exportContextReader{ctx,stream},int64(file.UncompressedSize64)+1),buffer)
  closeErr:=stream.Close()
  if err:=ctx.Err();err!=nil{return nil,err};if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
  if count!=int64(file.UncompressedSize64){return nil,v2input.ErrInvalid}
  expanded+=count
  files=append(files,VerifiedExportZIPFile{Path:file.Name,Bytes:count,SHA256:hex.EncodeToString(digest.Sum(nil))})
 }
 if !manifest{return nil,v2input.ErrInvalid}
 return files,ctx.Err()
}

// Bound the eager archive/zip central-directory parser before constructing it.
// Exporter omits archive comments, so EOCD must be the last 22 physical bytes.
func exportZIPMetadataBudget(source io.ReaderAt,size int64,maxFiles int)error{
 read:=func(offset int64,length int)([]byte,error){if offset<0||offset>size-int64(length){return nil,v2input.ErrInvalid};raw:=make([]byte,length);_,err:=source.ReadAt(raw,offset);return raw,err}
 raw,err:=read(size-22,22);if err!=nil{return err}
 u16:=func(raw []byte,offset int)uint16{return binary.LittleEndian.Uint16(raw[offset:])}
 u32:=func(raw []byte,offset int)uint32{return binary.LittleEndian.Uint32(raw[offset:])}
 u64:=func(raw []byte,offset int)uint64{return binary.LittleEndian.Uint64(raw[offset:])}
 if u32(raw,0)!=0x06054b50||u16(raw,4)!=0||u16(raw,6)!=0||u16(raw,8)!=u16(raw,10)||u16(raw,20)!=0{return v2input.ErrInvalid}
 count,directory,offset:=uint64(u16(raw,10)),uint64(u32(raw,12)),uint64(u32(raw,16));end:=uint64(size-22)
 if count==65535||directory==0xffffffff||offset==0xffffffff{
  locator,err:=read(size-42,20);if err!=nil{return err}
  if u32(locator,0)!=0x07064b50||u32(locator,4)!=0||u32(locator,16)!=1{return v2input.ErrInvalid}
  location:=u64(locator,8);if location>uint64(size)||location>uint64(size-42){return v2input.ErrInvalid}
  record,err:=read(int64(location),56);if err!=nil{return err}
  length:=u64(record,4)
  if u32(record,0)!=0x06064b50||length<44||length>64<<20||location+12+length!=uint64(size-42)||u32(record,16)!=0||u32(record,20)!=0||u64(record,24)!=u64(record,32){return v2input.ErrInvalid}
  count,directory,offset,end=u64(record,32),u64(record,40),u64(record,48),location
 }
 if count<1||count>uint64(maxFiles)||directory>64<<20||offset>end||directory>end-offset||offset+directory!=end{return v2input.ErrInvalid}
 return nil
}
