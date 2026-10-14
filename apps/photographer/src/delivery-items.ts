import type {DeliveryPackage} from './delivery-package';
export type DeliveryItem={id:string;ordinal:number;preset_id:string;preset_sha:string;required:boolean;export_id:string|null;state:'pending'|'ready'|'failed'|'omitted';reason:string|null};
export type DeliveryCursor={ordinal:number;preset_id:string};
export type DeliveryItemsPage={items:DeliveryItem[];next:DeliveryCursor|null};
const uuid=(v:unknown):v is string=>typeof v==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v)&&v!=='00000000-0000-0000-0000-000000000000';
const fields=(v:unknown,keys:string[]):v is Record<string,unknown>=>!!v&&typeof v==='object'&&!Array.isArray(v)&&Object.keys(v).length===keys.length&&keys.every(k=>Object.prototype.hasOwnProperty.call(v,k));
const cursor=(v:unknown):v is DeliveryCursor=>fields(v,['ordinal','preset_id'])&&Number.isInteger(v.ordinal)&&(v.ordinal as number)>=0&&(v.ordinal as number)<5000&&uuid(v.preset_id);
const later=(a:DeliveryCursor,b:DeliveryCursor)=>a.ordinal>b.ordinal||a.ordinal===b.ordinal&&a.preset_id>b.preset_id;
function invalid():never{throw new Error('DELIVERY_ITEMS_INVALID');}
export function readDeliveryItemsPage(value:unknown,pkg:DeliveryPackage,after:DeliveryCursor|null):DeliveryItemsPage{
 if(after!==null&&!cursor(after))return invalid();
 if(!fields(value,['package_id','revision','manifest','items','next'])||value.package_id!==pkg.id||value.revision!==pkg.revision||!Array.isArray(value.items)||value.items.length>200)return invalid();
 if(pkg.document===null?value.manifest!==null:!fields(value.manifest,['artifact_id','sha256'])||value.manifest.artifact_id!==pkg.document.artifact_id||value.manifest.sha256!==pkg.document.sha256)return invalid();
 const items:DeliveryItem[]=[],ids=new Set<string>();let previous=after;
 for(const item of value.items){
  if(!fields(item,['id','ordinal','preset_id','preset_sha','required','export_id','state','reason'])||!uuid(item.id)||!Number.isInteger(item.ordinal)||(item.ordinal as number)<0||(item.ordinal as number)>=5000||!uuid(item.preset_id)||typeof item.preset_sha!=='string'||!/^[0-9a-f]{64}$/.test(item.preset_sha)||typeof item.required!=='boolean'||item.export_id!==null&&!uuid(item.export_id)||!['pending','ready','failed','omitted'].includes(item.state as string)||item.reason!==null&&(typeof item.reason!=='string'||!item.reason.trim()||[...item.reason].length>256))return invalid();
  if((item.state==='pending'||item.state==='ready')&&item.reason!==null||item.state==='ready'&&item.export_id===null||item.state==='omitted'&&(item.required||item.export_id!==null)||(item.state==='failed'||item.state==='omitted')&&item.reason===null||ids.has(item.id))return invalid();
  const position={ordinal:item.ordinal as number,preset_id:item.preset_id};
  if(previous&&!later(position,previous))return invalid();
  previous=position;ids.add(item.id);items.push({...item} as DeliveryItem);
 }
 let next:DeliveryCursor|null=null;
 if(value.next!==null){
  if(!cursor(value.next)||items.length===0||!previous||value.next.ordinal!==previous.ordinal||value.next.preset_id!==previous.preset_id)return invalid();
  next={...value.next};
 }
 return {items,next};
}
/** Only a terminal page completes inspection; a failed page exposes no partial set. */
export async function collectDeliveryItems(pkg:DeliveryPackage,load:(after:DeliveryCursor|null)=>Promise<unknown>,signal?:AbortSignal):Promise<DeliveryItem[]>{
 const owned=structuredClone(pkg),all:DeliveryItem[]=[],seen=new Set<string>();let after:DeliveryCursor|null=null;
 for(let page=0;page<=80000;page++){
  if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
  const raw=await load(after?{...after}:null);
  if(signal?.aborted)throw new Error('DELIVERY_ABORTED');
  const result=readDeliveryItemsPage(raw,owned,after);
  for(const item of result.items){if(seen.has(item.id)||all.length>=80000)return invalid();seen.add(item.id);all.push(item)}
  if(result.next===null)return all;
  after=result.next;
 }
 return invalid();
}
