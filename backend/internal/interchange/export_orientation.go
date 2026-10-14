package interchange
import (
 "bytes"
 "context"
 "encoding/binary"
 "io"
 "hash/crc32"
 "path"
 "strings"
 "photoeditor/backend/internal/v2input"
)
// Encoded orientation witness, independent of XMP. This is metadata validation;
// full native raster/ICC/pixel qualification remains required at worker install.
func exportEncodedOrientation(ctx context.Context,source io.ReaderAt,size int64,name string)(int,error){
 if err:=ctx.Err();err!=nil{return 0,err};if source==nil||size<8||size>64<<30{return 0,v2input.ErrInvalid}
 read:=func(offset int64,n int)([]byte,error){if err:=ctx.Err();err!=nil{return nil,err};if offset<0||int64(n)>size-offset{return nil,v2input.ErrInvalid};raw:=make([]byte,n);_,err:=source.ReadAt(raw,offset);return raw,err}
 suffix:=strings.ToLower(path.Ext(name))
 if suffix==".tif"||suffix==".tiff"{return exportTIFFOrientation(ctx,source,size)}
 if suffix==".jpg"||suffix==".jpeg"{
  magic,err:=read(0,2);if err!=nil||!bytes.Equal(magic,[]byte{255,216}){return 0,v2input.ErrInvalid}
  offset:=int64(2);orientation:=1;found:=false
  for count:=0;count<30000;count++{
   marker,err:=read(offset,2);if err!=nil{return 0,err};offset+=2
   if marker[0]!=255{return 0,v2input.ErrInvalid}
   if marker[1]==255{offset--;continue}
   if marker[1]==0xda||marker[1]==0xd9{return orientation,nil}
   if marker[1]==0x01||(marker[1]>=0xd0&&marker[1]<=0xd7){continue}
   lengthRaw,err:=read(offset,2);if err!=nil{return 0,err};length:=int64(binary.BigEndian.Uint16(lengthRaw));if length<2||length>size-offset{return 0,v2input.ErrInvalid}
   if marker[1]==0xe1&&length>=8{
    header,err:=read(offset+2,6);if err!=nil{return 0,err}
    if bytes.Equal(header,[]byte("Exif\x00\x00")){
     if found{return 0,v2input.ErrInvalid};found=true
     orientation,err=exportTIFFOrientation(ctx,io.NewSectionReader(source,offset+8,length-8),length-8);if err!=nil{return 0,err}
    }
   }
   offset+=length
  }
  return 0,v2input.ErrInvalid
 }
 if suffix==".png"{
  magic,err:=read(0,8);if err!=nil||!bytes.Equal(magic,[]byte{137,80,78,71,13,10,26,10}){return 0,v2input.ErrInvalid}
  offset:=int64(8);orientation:=1;found:=false
  for count:=0;count<30000;count++{
   header,err:=read(offset,8);if err!=nil{return 0,err};length:=int64(binary.BigEndian.Uint32(header));if length>size-offset-12{return 0,v2input.ErrInvalid}
   kind:=string(header[4:]);if kind=="eXIf"{
    if found{return 0,v2input.ErrInvalid};found=true
    crc:=crc32.NewIEEE();_,_=crc.Write(header[4:])
    if _,err=io.CopyBuffer(crc,exportContextReader{ctx,io.NewSectionReader(source,offset+8,length)},make([]byte,32<<10));err!=nil{return 0,err}
    checksum,err:=read(offset+8+length,4);if err!=nil{return 0,err};if binary.BigEndian.Uint32(checksum)!=crc.Sum32(){return 0,v2input.ErrInvalid}
    orientation,err=exportTIFFOrientation(ctx,io.NewSectionReader(source,offset+8,length),length);if err!=nil{return 0,err}
   }
   offset+=length+12
   if kind=="IEND"{if length!=0||offset!=size{return 0,v2input.ErrInvalid};return orientation,nil}
  }
 }
 return 0,v2input.ErrInvalid
}
func exportTIFFOrientation(ctx context.Context,source io.ReaderAt,size int64)(int,error){
 read:=func(offset uint64,n int)([]byte,error){if err:=ctx.Err();err!=nil{return nil,err};if size<0||offset>uint64(size)||uint64(n)>uint64(size)-offset{return nil,v2input.ErrInvalid};raw:=make([]byte,n);_,err:=source.ReadAt(raw,int64(offset));return raw,err}
 header,err:=read(0,8);if err!=nil{return 0,err}
 var order binary.ByteOrder
 switch string(header[:2]){case "II":order=binary.LittleEndian;case "MM":order=binary.BigEndian;default:return 0,v2input.ErrInvalid}
 kind:=order.Uint16(header[2:]);offset:=uint64(order.Uint32(header[4:]));countBytes,entryBytes,valueOffset:=2,12,8;big:=false
 if kind==43{
  big=true;if order.Uint16(header[4:])!=8||order.Uint16(header[6:])!=0{return 0,v2input.ErrInvalid}
  raw,err:=read(8,8);if err!=nil{return 0,err};offset=order.Uint64(raw);countBytes,entryBytes,valueOffset=8,20,12
 }else if kind!=42{return 0,v2input.ErrInvalid}
 minimum:=uint64(8);if big{minimum=16};if offset<minimum{return 0,v2input.ErrInvalid}
 raw,err:=read(offset,countBytes);if err!=nil{return 0,err};count:=uint64(order.Uint16(raw));if big{count=order.Uint64(raw)}
 if count>4096||uint64(countBytes)+count*uint64(entryBytes)>uint64(size)-offset{return 0,v2input.ErrInvalid}
 orientation:=1;found:=false
 for i:=uint64(0);i<count;i++{
  raw,err:=read(offset+uint64(countBytes)+i*uint64(entryBytes),entryBytes);if err!=nil{return 0,err}
  if order.Uint16(raw)!=274{continue}
  n:=uint64(order.Uint32(raw[4:]));if big{n=order.Uint64(raw[4:])}
  if found||order.Uint16(raw[2:])!=3||n!=1{return 0,v2input.ErrInvalid};found=true
  orientation=int(order.Uint16(raw[valueOffset:]));if orientation<1||orientation>8{return 0,v2input.ErrInvalid}
 }
 return orientation,nil
}
