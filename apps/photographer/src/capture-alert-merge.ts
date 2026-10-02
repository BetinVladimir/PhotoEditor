import type {CaptureAlert} from './capture-types';

// Revisions are canonical decimal strings and may exceed JS safe integers.
function newer(left:string,right:string){
 return left.length!==right.length?left.length>right.length:left>right;
}
export function mergeCaptureAlerts(current:CaptureAlert[],incoming:CaptureAlert[]):CaptureAlert[]{
 const byId=new Map(current.map(alert=>[alert.id,alert]));
 for(const alert of incoming){
  const previous=byId.get(alert.id);
  if(!previous)byId.set(alert.id,alert);
  else{
   const latest=newer(alert.revision,previous.revision)?alert:previous;
   // Confirmation is immutable and can arrive without an alert revision change.
   const confirmation=previous.confirmation_id??alert.confirmation_id;
   byId.set(alert.id,confirmation?{...latest,confirmation_id:confirmation}:latest);
  }
 }
 return [...byId.values()].sort((a,b)=>b.created_at.localeCompare(a.created_at)||b.id.localeCompare(a.id));
}
