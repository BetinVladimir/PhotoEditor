import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {VerifiedRegionManifest} from './region-preview-manifest';
import {regionRenderedScale} from './region-preview-view';
export type RegionDisplay={uri:string;width:number;height:number;scale:ReturnType<typeof regionRenderedScale>;dispose():void};
export type RegionBrowserDecoder={decode(blob:Blob):Promise<{width:number;height:number;close():void}>;createURL(blob:Blob):string;revokeURL(url:string):void};
const browserDecoder:RegionBrowserDecoder={
 decode:blob=>{if(typeof createImageBitmap!=='function')throw new Error('REGION_DECODER_UNAVAILABLE');return createImageBitmap(blob);},
 createURL:blob=>URL.createObjectURL(blob),revokeURL:url=>URL.revokeObjectURL(url),
};
/** Browser default color conversion retains the embedded profile; never decode master bytes. */
export async function prepareRegionDisplay(manifest:VerifiedRegionManifest,encodedBytes:Uint8Array,signal:AbortSignal,decoder:RegionBrowserDecoder=browserDecoder):Promise<RegionDisplay> {
 const expected=structuredClone(manifest);
 const active=()=>{if(signal.aborted)throw new Error('REGION_PREVIEW_ABORTED');};active();
 const scale=regionRenderedScale(expected.geometry);
 if(encodedBytes.length!==expected.bytes||encodedBytes.length<2||encodedBytes.length>16777216||!['image/jpeg','image/webp'].includes(expected.mediaType))throw new Error('REGION_DISPLAY_BYTES_INVALID');
 const owned=new Uint8Array(encodedBytes.length);owned.set(encodedBytes);
 if(bytesToHex(sha256(owned))!==expected.derivative.sha256)throw new Error('REGION_DISPLAY_BYTES_INVALID');
 const blob=new Blob([owned],{type:expected.mediaType});
 const image=await decoder.decode(blob);
 try{
  active();if(image.width!==expected.geometry.output_width||image.height!==expected.geometry.output_height)throw new Error('REGION_DISPLAY_DIMENSIONS_INVALID');
 }finally{image.close();}
 active();const uri=decoder.createURL(blob);let disposed=false;
 const dispose=()=>{if(disposed)return;disposed=true;signal.removeEventListener('abort',dispose);decoder.revokeURL(uri);};
 signal.addEventListener('abort',dispose,{once:true});
 if(signal.aborted){dispose();active();}
 return {uri,width:expected.geometry.output_width,height:expected.geometry.output_height,scale,dispose};
}
