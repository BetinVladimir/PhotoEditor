import type {DeliveryPackage} from './delivery-package';
import {readDeliveryItemsPage,type DeliveryItem,type DeliveryCursor} from './delivery-items';
import type {ArtifactRef} from './catalog-types';
export type DeliveryInspection={package_id:string;revision:string;manifest:ArtifactRef;slot_count:number;ready_count:number;omitted_count:number};
function invalid():never{throw new Error('DELIVERY_INSPECTION_INVALID');}
/** An observation bound to the complete displayed set; final publication reauthorizes. */
export function readDeliveryInspection(value:unknown,pkg:DeliveryPackage,items:readonly DeliveryItem[]):DeliveryInspection{
 if(!value||typeof value!=='object'||Array.isArray(value))return invalid();
 const v=value as Record<string,unknown>,keys=['package_id','revision','manifest','slot_count','ready_count','omitted_count'];
 if(Object.keys(v).length!==keys.length||!keys.every(k=>Object.prototype.hasOwnProperty.call(v,k))||v.package_id!==pkg.id||v.revision!==pkg.revision||pkg.document===null||pkg.published_at!==null||!['review','ready'].includes(pkg.state))return invalid();
 const manifest=v.manifest as Record<string,unknown>|null;
 if(!manifest||typeof manifest!=='object'||Array.isArray(manifest)||Object.keys(manifest).length!==2||manifest.artifact_id!==pkg.document.artifact_id||manifest.sha256!==pkg.document.sha256)return invalid();
 const total=v.slot_count,ready=v.ready_count,omitted=v.omitted_count;
 if(!Number.isInteger(total)||!Number.isInteger(ready)||!Number.isInteger(omitted)||(total as number)<1||(total as number)>80000||(ready as number)<1||(ready as number)>(total as number)||(omitted as number)<0||(omitted as number)>(total as number)||(ready as number)+(omitted as number)!==total||items.length!==total)return invalid();
 let readyCount=0,omittedCount=0,after:DeliveryCursor|null=null;const ids=new Set<string>();
 for(let start=0;start<items.length;start+=200){
  const page=readDeliveryItemsPage({package_id:pkg.id,revision:pkg.revision,manifest:pkg.document,items:items.slice(start,start+200),next:null},pkg,after);
  for(const item of page.items){
   if(ids.has(item.id))return invalid();ids.add(item.id);
   if(item.state==='ready')readyCount++;
   else if(item.state==='omitted'&&!item.required)omittedCount++;
   else return invalid();
   after={ordinal:item.ordinal,preset_id:item.preset_id};
  }
 }
 if(readyCount!==ready||omittedCount!==omitted)return invalid();
 return {package_id:pkg.id,revision:pkg.revision,manifest:{...pkg.document},slot_count:total as number,ready_count:ready as number,omitted_count:omitted as number};
}
