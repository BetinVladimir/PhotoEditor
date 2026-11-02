import type {EditResult,TimelineDocument} from '@photoeditor/api-client';
import {clipDuration,frameTicks} from './video-timeline-time';
import {splitClip} from './video-timeline-commands';

export type CutBoundary={id:string;start_ticks:number;end_ticks:number};
export type ApplyCutsMode='split_at_start'|'split_at_middle'|'markers_only';
export type ApplyCutsResult=EditResult;

/** FN-T-10: apply accepted boundaries from the end of the timeline backwards. Idempotent for a repeated report. */
export function applyCuts(src:TimelineDocument,mediaId:string,boundaries:CutBoundary[],accepted:string[],mode:ApplyCutsMode,newIds:string[]):ApplyCutsResult{
 let doc:TimelineDocument=JSON.parse(JSON.stringify(src));
 const FT=frameTicks(doc.video.fps),ids=newIds[Symbol.iterator](),applied:NonNullable<Extract<EditResult,{ok:true}>['applied']>=[];
 const take=()=>{const n=ids.next();return n.done?null:n.value;};
 const picked=boundaries.filter(b=>accepted.includes(b.id)).sort((a,b)=>b.start_ticks-a.start_ticks);
 for(const b of picked){
  const ts=mode==='split_at_middle'?Math.floor((b.start_ticks+b.end_ticks)/2):b.start_ticks;
  for(const t of doc.tracks){
   if(t.kind!=='video')continue;
   for(const c of [...t.clips]){
    const s=c.source;
    if(!('media_id' in s)||s.media_id!==mediaId)continue;
    const si=c.src_in as number,so=c.src_out as number;
    if(!(si<ts&&ts<so))continue;
    const sp=c.speed??{num:1,den:1};
    let tl=c.at+Math.floor(((ts-si)*sp.den)/sp.num);
    tl=Math.floor((tl+Math.floor(FT/2))/FT)*FT;
    if(!(c.at<tl&&tl<c.at+clipDuration(c)))continue;
    if(mode==='markers_only'){
     if(doc.markers.some(m=>m.at===tl&&m.kind==='cut'))continue;
     const nid=take();if(nid===null)return{ok:false,error:'APPLY_IDS_EXHAUSTED'};
     doc.markers.push({id:nid,at:tl,kind:'cut',label:''});doc.markers.sort((a,b)=>a.at-b.at);// vectors TC-T-AC-M1: markers are kept ordered by time
     applied.push({boundary:b.id,clip:c.id,new:nid,at:tl});
     continue;
    }
    const nid=take();if(nid===null)return{ok:false,error:'APPLY_IDS_EXHAUSTED'};
    const r=splitClip(doc,c.id,tl,nid);
    if(!r.ok)return r;
    doc=r.doc;applied.push({boundary:b.id,clip:c.id,new:nid,at:tl});
   }
  }
 }
 return{ok:true,doc,applied};
}
