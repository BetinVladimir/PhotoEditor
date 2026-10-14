package interchange
import (
 "bytes"
 "context"
 "encoding/binary"
 "testing"
 "hash/crc32"
)
func exportTIFFTestBytes(orientation int)[]byte{
 raw:=make([]byte,26);copy(raw,[]byte{'I','I',42,0,8,0,0,0});binary.LittleEndian.PutUint16(raw[8:],1)
 binary.LittleEndian.PutUint16(raw[10:],274);binary.LittleEndian.PutUint16(raw[12:],3);binary.LittleEndian.PutUint32(raw[14:],1);binary.LittleEndian.PutUint16(raw[18:],uint16(orientation));return raw
}
func TestExportEncodedOrientationIndependentTIFFAndJPEG(t *testing.T){
 for orientation:=1;orientation<=8;orientation++{
  tiff:=exportTIFFTestBytes(orientation)
  jpeg:=[]byte{255,216,255,225,0,byte(len(tiff)+8)};jpeg=append(jpeg,[]byte("Exif\x00\x00")...);jpeg=append(jpeg,tiff...);jpeg=append(jpeg,255,217)
  for name,raw:=range map[string][]byte{"image.tiff":tiff,"image.jpg":jpeg}{
   got,err:=exportEncodedOrientation(context.Background(),bytes.NewReader(raw),int64(len(raw)),name);if err!=nil||got!=orientation{t.Fatal(name,orientation,err)}
  }
 }
}
func TestExportEncodedOrientationRejectsInvalidValuesAndOffsets(t *testing.T){
 for _,kind:=range []string{"orientation","offset","type","truncated","suffix"}{
  raw:=exportTIFFTestBytes(1);name:="image.tiff"
  switch kind{case "orientation":raw[18]=9;case "offset":raw[4]=255;case "type":raw[12]=4;case "truncated":raw=raw[:10];case "suffix":name="image.png"}
  if _,err:=exportEncodedOrientation(context.Background(),bytes.NewReader(raw),int64(len(raw)),name);err==nil{t.Fatal("invalid metadata accepted",kind)}
 }
}

func exportPNGTestChunk(kind string,payload []byte)[]byte{
 raw:=make([]byte,12+len(payload));binary.BigEndian.PutUint32(raw,uint32(len(payload)));copy(raw[4:],kind);copy(raw[8:],payload)
 binary.BigEndian.PutUint32(raw[8+len(payload):],crc32.ChecksumIEEE(raw[4:8+len(payload)]));return raw
}
func TestExportEncodedOrientationPNGAndBigTIFFBothByteOrders(t *testing.T){
 for _,order:=range []binary.ByteOrder{binary.LittleEndian,binary.BigEndian}{
  for orientation:=1;orientation<=8;orientation++{
   raw:=make([]byte,52);if order==binary.LittleEndian{copy(raw,"II")}else{copy(raw,"MM")}
   order.PutUint16(raw[2:],43);order.PutUint16(raw[4:],8);order.PutUint64(raw[8:],16);order.PutUint64(raw[16:],1)
   order.PutUint16(raw[24:],274);order.PutUint16(raw[26:],3);order.PutUint64(raw[28:],1);order.PutUint16(raw[36:],uint16(orientation))
   got,err:=exportEncodedOrientation(context.Background(),bytes.NewReader(raw),int64(len(raw)),"big.tiff")
   if err!=nil||got!=orientation{t.Fatal("BigTIFF orientation",orientation,err)}
   png:=[]byte{137,80,78,71,13,10,26,10};png=append(png,exportPNGTestChunk("eXIf",exportTIFFTestBytes(orientation))...);png=append(png,exportPNGTestChunk("IEND",nil)...)
   got,err=exportEncodedOrientation(context.Background(),bytes.NewReader(png),int64(len(png)),"image.png")
   if err!=nil||got!=orientation{t.Fatal("PNG orientation",orientation,err)}
   png[20]^=1
   if _,err:=exportEncodedOrientation(context.Background(),bytes.NewReader(png),int64(len(png)),"image.png");err==nil{t.Fatal("invalid eXIf CRC accepted")}
  }
 }
}
