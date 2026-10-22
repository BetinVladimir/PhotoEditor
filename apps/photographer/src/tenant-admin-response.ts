import {readInvitationReceipt,type InvitationRequest} from './invitation-attempt-store';
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).sort().join(',')===[...keys].sort().join(',');
export function eventCursor(value:unknown):string{
 if(typeof value!=='string'||!/^(0|[1-9][0-9]{0,18})$/.test(value)||BigInt(value)>9223372036854775807n)throw new Error('TENANT_EVENT_INVALID');return value;
}
export type TenantEvent={cursor:string;type:string;run_id:string|null;collection_id:string|null;payload:unknown;created_at:string};
export function readTenantEvent(raw:unknown,after?:string):TenantEvent{
 if(!fields(raw,['cursor','type','run_id','collection_id','payload','created_at'])||typeof raw.type!=='string'||!raw.type||raw.type.length>256||(raw.run_id!==null&&!uuid(raw.run_id))||(raw.collection_id!==null&&!uuid(raw.collection_id))||typeof raw.created_at!=='string'||!/^\d{4}-\d\d-\d\dT/.test(raw.created_at)||!Number.isFinite(Date.parse(raw.created_at)))throw new Error('TENANT_EVENT_INVALID');
 const cursor=eventCursor(raw.cursor);if(after!==undefined&&BigInt(cursor)<=BigInt(eventCursor(after)))throw new Error('TENANT_EVENT_INVALID');
 return {cursor,type:raw.type,run_id:raw.run_id,collection_id:raw.collection_id,payload:JSON.parse(JSON.stringify(raw.payload)),created_at:raw.created_at};
}
export function readTenantEventPage(raw:unknown,after?:string){
 if(!fields(raw,['items','next_cursor'])||!Array.isArray(raw.items)||raw.items.length>100||typeof raw.next_cursor!=='string')throw new Error('TENANT_EVENT_INVALID');
 let last=after;const items=raw.items.map(value=>{const row=readTenantEvent(value,last);last=row.cursor;return row;});
 if(raw.next_cursor!==(last??''))throw new Error('TENANT_EVENT_INVALID');return {items,next_cursor:raw.next_cursor};
}
export function freezeTenantInvitation(email:string,role:InvitationRequest['role'],locale:InvitationRequest['locale']):InvitationRequest{
 if(typeof email!=='string'||!['admin','editor','viewer'].includes(role)||!['en','ru','bg'].includes(locale))throw new Error('INVITATION_REQUEST_INVALID');return {email,role,locale};
}
export {readInvitationReceipt};
