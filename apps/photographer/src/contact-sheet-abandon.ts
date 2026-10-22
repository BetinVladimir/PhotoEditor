import type {ApiClient} from '@photoeditor/api-client';
import type {ContactSheetAttempt} from './contact-sheet-attempt-store';
import {readContactSheetAccepted,type ContactSheetAccepted} from './contact-sheet-response';
export type ContactSheetResolution={status:'abandoned'}|{status:'accepted';ticket:ContactSheetAccepted};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
// The request is the original frozen admission, including its original key.
// No new selection or key is allocated while closing an uncertain attempt.
export async function abandonContactSheet(client:Pick<ApiClient,'request'>,tenant:string,attempt:ContactSheetAttempt,signal:AbortSignal):Promise<ContactSheetResolution>{
 signal.throwIfAborted();
 if(!uuid(tenant)||!attempt||attempt.schema_version!==1||!uuid(attempt.collection_id)||!uuid(attempt.key)||!attempt.request||Object.keys(attempt.request).sort().join(',')!=='columns,locale,rows,selection_snapshot_id'||!uuid(attempt.request.selection_snapshot_id)||!['en','ru','bg'].includes(attempt.request.locale)||attempt.request.columns!==4||attempt.request.rows!==5||attempt.ticket!==null&&(!attempt.ticket||!uuid(attempt.ticket.run_id)))throw Error('CONTACT_SHEET_RESOLUTION_INVALID');
 const key=attempt.key,request={...attempt.request},knownRun=attempt.ticket?.run_id;
 const raw=await client.request<unknown>(`/api/v1/tenants/${tenant}/contact-sheets/abandon`,{method:'POST',body:{...request},idempotencyKey:key,signal});
 signal.throwIfAborted();
 if(raw&&typeof raw==='object'&&!Array.isArray(raw)&&(raw as Record<string,unknown>).status==='abandoned'){
  const value=raw as Record<string,unknown>;
  if(Object.keys(value).sort().join(',')!=='key,selection_snapshot_id,status'||value.key!==key||value.selection_snapshot_id!==request.selection_snapshot_id||knownRun)throw Error('CONTACT_SHEET_RESOLUTION_INVALID');
  return {status:'abandoned'};
 }
 const ticket=readContactSheetAccepted(raw);
 if(knownRun&&ticket.run_id!==knownRun)throw Error('CONTACT_SHEET_RESOLUTION_INVALID');
 return {status:'accepted',ticket};
}
