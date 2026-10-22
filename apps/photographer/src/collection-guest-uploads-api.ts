import type {ApiClient} from '@photoeditor/api-client';

export type GuestUploadRow={id:string;collection_id:string;share_id:string;filename:string;state:string;revision:string;expected_size:number;actual_size?:number;created_at:string;item_id?:string;asset_version_id?:string;moderation_state?:'pending_review'|'accepted'|'rejected'};
export type GuestUploadPage={items:GuestUploadRow[];next_cursor:string|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v);
const revision=(v:unknown):v is string=>typeof v==='string'&&/^[1-9][0-9]{0,18}$/.test(v)&&(v.length<19||v<='9223372036854775807');
const size=(v:unknown):v is number=>Number.isSafeInteger(v)&&Number(v)>0;
const timestamp=(v:unknown):v is string=>typeof v==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v)&&Number.isFinite(Date.parse(v));
export function decodeGuestUpload(raw:unknown,collection:string):GuestUploadRow{
 const allowed=['id','collection_id','share_id','filename','state','revision','expected_size','actual_size','created_at','item_id','asset_version_id','moderation_state'];
 if(!object(raw)||Object.keys(raw).some(k=>!allowed.includes(k))||!uuid(raw.id)||raw.collection_id!==collection||!uuid(raw.share_id)||typeof raw.filename!=='string'||!raw.filename||raw.filename.length>4096||typeof raw.state!=='string'||!['initializing','uploading','completing','verifying','verified','rejected','aborted','expired'].includes(raw.state)||!revision(raw.revision)||!size(raw.expected_size)||raw.actual_size!==undefined&&(!size(raw.actual_size)||raw.actual_size>raw.expected_size)||!timestamp(raw.created_at))throw new Error('GUEST_UPLOAD_INVALID');
 const membership=raw.item_id!==undefined||raw.asset_version_id!==undefined||raw.moderation_state!==undefined;
 if(membership&&(!uuid(raw.item_id)||!uuid(raw.asset_version_id)||!['pending_review','accepted','rejected'].includes(raw.moderation_state as string)))throw new Error('GUEST_UPLOAD_INVALID');
 return {...raw} as GuestUploadRow;
}
export class CollectionGuestUploadsAPI{
 private root:string;
 constructor(private client:Pick<ApiClient,'request'>,private tenant:string,private collection:string){if(!uuid(tenant)||!uuid(collection))throw new Error('GUEST_UPLOAD_SCOPE_INVALID');this.root=`/api/v1/tenants/${tenant}/collections/${collection}`;}
 async list(signal:AbortSignal,cursor?:string):Promise<GuestUploadPage>{
  signal.throwIfAborted();if(cursor!==undefined&&(!cursor||cursor.length>4096))throw new Error('GUEST_UPLOAD_CURSOR_INVALID');
  const raw=await this.client.request<unknown>(`${this.root}/guest-uploads?limit=100${cursor?'&cursor='+encodeURIComponent(cursor):''}`,{signal});signal.throwIfAborted();
  if(!object(raw)||Object.keys(raw).sort().join(',')!=='items,next_cursor'||!Array.isArray(raw.items)||raw.items.length>100||raw.next_cursor!==null&&(typeof raw.next_cursor!=='string'||!raw.next_cursor||raw.next_cursor.length>4096||raw.next_cursor===cursor))throw new Error('GUEST_UPLOAD_PAGE_INVALID');
  const items=raw.items.map(v=>decodeGuestUpload(v,this.collection));if(new Set(items.map(v=>v.id)).size!==items.length||raw.next_cursor!==null&&!items.length)throw new Error('GUEST_UPLOAD_PAGE_INVALID');
  return {items,next_cursor:raw.next_cursor as string|null};
 }
 async moderate(row:GuestUploadRow,decision:'accept'|'reject',signal:AbortSignal){
  signal.throwIfAborted();const frozen=decodeGuestUpload(row,this.collection);
  if(!['accept','reject'].includes(decision)||frozen.state!=='verified'||frozen.moderation_state!=='pending_review'||!frozen.item_id||!Number.isSafeInteger(Number(frozen.revision)))throw new Error('GUEST_UPLOAD_NOT_REVIEWABLE');
  const raw=await this.client.request<unknown>(`/api/v1/tenants/${this.tenant}/uploads/${frozen.id}/moderation`,{method:'POST',body:{decision,expected_revision:Number(frozen.revision)},signal});signal.throwIfAborted();
  if(!object(raw)||Object.keys(raw).sort().join(',')!=='asset_id,asset_version_id,collection_id,id,moderation_state,position,visibility'||raw.id!==frozen.item_id||raw.collection_id!==this.collection||!uuid(raw.asset_id)||raw.asset_version_id!==frozen.asset_version_id||raw.visibility!=='hidden'||raw.moderation_state!==(decision==='accept'?'accepted':'rejected')||!Number.isSafeInteger(raw.position)||Number(raw.position)<0)throw new Error('GUEST_UPLOAD_RECEIPT_INVALID');
 }
}
