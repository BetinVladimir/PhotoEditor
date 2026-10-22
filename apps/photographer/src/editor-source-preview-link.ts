import type {EditorSourcePreview} from './editor-source-preview-poll';
/** Decode the exact requested source instead of accepting the first matching row. */
export function readEditorSourcePreview(value:unknown,version:string,now=Date.now()):EditorSourcePreview{
 const fail=():never=>{throw new Error('EDITOR_SOURCE_PREVIEW_INVALID')};
 if(!value||typeof value!=='object'||Array.isArray(value))return fail();
 const items=(value as Record<string,unknown>).items;
 if(!Array.isArray(items)||items.length!==1)return fail();
 const row=items[0];if(!row||typeof row!=='object'||Array.isArray(row))return fail();
 const item=row as Record<string,unknown>;
 if(item.variant!=='preview1600'||item.asset_version_id!==version||typeof item.url!=='string'||item.url.length>8192||typeof item.expires_at!=='string'||!Number.isFinite(Date.parse(item.expires_at))||Date.parse(item.expires_at)<=now||!['image/jpeg','image/webp'].includes(item.media_type as string)||!Number.isSafeInteger(item.width)||!Number.isSafeInteger(item.height)||Number(item.width)<1||Number(item.height)<1||Math.max(Number(item.width),Number(item.height))>1600)return fail();
 let url:URL;try{url=new URL(item.url)}catch{return fail()}
 if(!url.hostname||url.username||url.password||url.hash||!(url.protocol==='https:'||url.protocol==='http:'&&(['localhost','[::1]'].includes(url.hostname)||/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))))return fail();
 return {url:item.url,expires_at:item.expires_at};
}
