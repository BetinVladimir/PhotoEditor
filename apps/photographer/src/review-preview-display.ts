import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import type {ReviewPreviewBytes} from './review-preview';
const alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';
function base64(bytes:Uint8Array):string{
 const blocks:string[]=[];let block='';
 for(let i=0;i<bytes.length;i+=3){const a=bytes[i],b=bytes[i+1],c=bytes[i+2];block+=alphabet[a>>2]+alphabet[((a&3)<<4)|((b??0)>>4)]+(b===undefined?'=':alphabet[((b&15)<<2)|((c??0)>>6)])+(c===undefined?'=':alphabet[c&63]);if(block.length>=16384){blocks.push(block);block=''}}
 blocks.push(block);return blocks.join('');
}
export async function prepareReviewPreviewDisplay(value:ReviewPreviewBytes,signal:AbortSignal,decode:(uri:string)=>Promise<{width:number;height:number}>):Promise<{uri:string;width:number;height:number}>{
 const active=()=>{if(signal.aborted)throw new Error('REVIEW_PREVIEW_ABORTED')};active();
 const bytes=new Uint8Array(value.bytes),image={...value.image},width=value.width,height=value.height;
 if(bytes.length<2||bytes.length>52*1048576||bytes.length!==image.bytes||!['image/jpeg','image/webp'].includes(image.media_type)||bytesToHex(sha256(bytes))!==image.sha256)throw new Error('REVIEW_PREVIEW_INVALID');
 const uri=`data:${image.media_type};base64,${base64(bytes)}`;
 active();const actual=await decode(uri);active();
 if(actual.width!==width||actual.height!==height)throw new Error('REVIEW_PREVIEW_INVALID');
 return {uri,width,height};
}

// A candidate review preserves the source canvas. Refuse an incoherent pair
// before decoding either side or presenting a decision to the user.
export async function prepareReviewPreviewPair(before:ReviewPreviewBytes,after:ReviewPreviewBytes,signal:AbortSignal,decode:(uri:string)=>Promise<{width:number;height:number}>){
 if(signal.aborted)throw new Error('REVIEW_PREVIEW_ABORTED');
 if(!/^[0-9a-f]{64}$/.test(before.profile)||before.profile!==after.profile||before.width!==after.width||before.height!==after.height||before.source.artifact_id===after.source.artifact_id)throw new Error('REVIEW_PREVIEW_PAIR_INVALID');
 return Promise.all([prepareReviewPreviewDisplay(before,signal,decode),prepareReviewPreviewDisplay(after,signal,decode)]);
}
