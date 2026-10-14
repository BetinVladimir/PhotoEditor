export function readPersonMaskPreviewPNG(bytes:Uint8Array,maskJCS:string):{width:number;height:number}{
  const bad=()=>{throw new Error('INTERCHANGE_MASK_PREVIEW_INVALID');};
  if(bytes.length<33||bytes.length>8<<20||[137,80,78,71,13,10,26,10].some((byte,index)=>bytes[index]!==byte)||bytes[8]!==0||bytes[9]!==0||bytes[10]!==0||bytes[11]!==13||String.fromCharCode(...bytes.slice(12,16))!=='IHDR')bad();
  const width=((bytes[16]*0x1000000)+(bytes[17]<<16)+(bytes[18]<<8)+bytes[19])>>>0;
  const height=((bytes[20]*0x1000000)+(bytes[21]<<16)+(bytes[22]<<8)+bytes[23])>>>0;
  let mask:unknown;try{mask=JSON.parse(maskJCS);}catch{bad();}
  const canvas=(mask as {canvas?:{width?:unknown;height?:unknown}}|null)?.canvas;
  const sourceW=canvas?.width,sourceH=canvas?.height;
  if(typeof sourceW!=='number'||typeof sourceH!=='number'||!Number.isSafeInteger(sourceW)||!Number.isSafeInteger(sourceH)||sourceW<1||sourceH<1||width<1||height<1||width>1024||height>1024||Math.abs(width/sourceW-height/sourceH)>1/Math.min(sourceW,sourceH))bad();
  return {width,height};
}
