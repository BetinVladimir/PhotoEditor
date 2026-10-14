import type {CaptureAlert} from './capture-types';
import type {Page} from './catalog-types';

const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const validID=(value:unknown):value is string=>typeof value==='string'&&uuid.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const object=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
const timestamp=(value:unknown)=>typeof value==='string'&&value.length<=64&&/^\d{4}-\d{2}-\d{2}T/.test(value)&&Number.isFinite(Date.parse(value));

export function readCaptureAlertPage(value:unknown,sessionId:string):Page<CaptureAlert>{
 if(!object(value)||!Array.isArray(value.items)||value.items.length>100||
    !(value.next_cursor===null||typeof value.next_cursor==='string'&&value.next_cursor.length>0&&value.next_cursor.length<=8192))throw new Error('Invalid capture alert page');
 const seen=new Set<string>();
 const items=value.items.map(raw=>{
  if(!object(raw)||!validID(raw.id)||seen.has(raw.id)||raw.session_id!==sessionId||
     !validID(raw.asset_version_id)||!validID(raw.report_artifact_id)||
     !(raw.receipt_id===null||validID(raw.receipt_id))||
     !(raw.confirmation_id===undefined||raw.confirmation_id===null||validID(raw.confirmation_id))||
     typeof raw.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(raw.revision)||
     raw.revision.length===19&&raw.revision>'9223372036854775807'||
     !['info','warning','critical'].includes(raw.severity as string)||
     typeof raw.qa_stale!=='boolean'||!timestamp(raw.created_at)||
     !(raw.acknowledged_by===null||validID(raw.acknowledged_by))||
     !(raw.acknowledged_at===null||timestamp(raw.acknowledged_at))||
     (raw.acknowledged_by===null)!==(raw.acknowledged_at===null))throw new Error('Invalid capture alert');
  seen.add(raw.id);
  return {...raw} as unknown as CaptureAlert;
 });
 return {items,next_cursor:value.next_cursor as string|null};
}
