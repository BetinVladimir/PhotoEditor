import {deliveryRevision,deliveryTimestamp} from './delivery-package';
export type DeliverySummary={id:string;project_id:string;workflow_id:string;name:string;state:'building'|'review'|'ready'|'failed'|'cancelled';revision:string;created_at:string};
export type DeliveryPage={items:DeliverySummary[];next_cursor:string|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const instant=(value:string):bigint=>{
 const fraction=/\.(\d{1,9})(?:Z|[+-]\d{2}:\d{2})$/.exec(value)?.[1]?.padEnd(9,'0')??'000000000';
 return BigInt(Date.parse(value))*1000000n+BigInt(fraction.slice(3));
};
export function readDeliveryPage(value:unknown,project:string,seen:ReadonlySet<string>=new Set(),after?:Pick<DeliverySummary,'id'|'created_at'>):DeliveryPage{
 if(!uuid(project)||!value||typeof value!=='object'||Array.isArray(value))throw new Error('DELIVERY_LIST_INVALID');
 const page=value as Record<string,unknown>;
 if(Object.keys(page).length!==2||!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096)||page.items.length===0&&page.next_cursor!==null)throw new Error('DELIVERY_LIST_INVALID');
 const ids=new Set(seen);
 let previous=after;
 for(const item of page.items){
  if(!item||typeof item!=='object'||Array.isArray(item))throw new Error('DELIVERY_LIST_INVALID');
  const row=item as Record<string,unknown>;
  if(Object.keys(row).length!==7||!uuid(row.id)||!uuid(row.workflow_id)||row.project_id!==project||ids.has(row.id)||typeof row.name!=='string'||Array.from(row.name).length<1||Array.from(row.name).length>200||!['building','review','ready','failed','cancelled'].includes(row.state as string)||!deliveryRevision(row.revision)||!deliveryTimestamp(row.created_at))throw new Error('DELIVERY_LIST_INVALID');
  if(previous){
   if(!uuid(previous.id)||!deliveryTimestamp(previous.created_at))throw new Error('DELIVERY_LIST_INVALID');
   const currentTime=instant(row.created_at as string),priorTime=instant(previous.created_at);
   if(currentTime<priorTime||currentTime===priorTime&&(row.id as string)<=previous.id)throw new Error('DELIVERY_LIST_INVALID');
  }
  ids.add(row.id);
  previous={id:row.id as string,created_at:row.created_at as string};
 }
 return {items:page.items as DeliverySummary[],next_cursor:page.next_cursor as string|null};
}
