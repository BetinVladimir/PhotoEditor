import type {Clip,EditError,EditResult,TimelineDocument,Track} from '@photoeditor/api-client';
import {clipDuration,frameTicks} from './video-timeline-time';

const clone=<T,>(v:T):T=>JSON.parse(JSON.stringify(v));
const speedOf=(c:Clip)=>c.speed??{num:1,den:1};
const mulDiv=(a:number,b:number,c:number)=>Number((BigInt(a)*BigInt(b))/BigInt(c));
const modNonZero=(a:number,b:number,c:number)=>(BigInt(a)*BigInt(b))%BigInt(c)!==0n;
const fail=(error:EditError):EditResult=>({ok:false,error});
const gcd=(a:number,b:number):number=>b?gcd(b,a%b):a;
function find(doc:TimelineDocument,id:string):{t:Track;i:number;c:Clip}|null{
 for(const t of doc.tracks){const i=t.clips.findIndex(c=>c.id===id);if(i>=0)return{t,i,c:t.clips[i]};}
 return null;
}

/** FN-T-05 */
export function splitClip(src:TimelineDocument,clipId:string,at:number,newId:string):EditResult{
 const doc=clone(src),f=find(doc,clipId);if(!f)return fail('CLIP_NOT_FOUND');
 const {t,i,c}=f,FT=frameTicks(doc.video.fps);
 if(c.source.kind==='photo')return fail('SPLIT_PHOTO_UNSUPPORTED');
 const d=clipDuration(c);
 if(!(c.at<at&&at<c.at+d))return fail('SPLIT_OUT_OF_RANGE');
 if(at%FT!==0)return fail('SPLIT_NOT_ALIGNED');
 const sp=speedOf(c),off=at-c.at;
 if(modNonZero(off,sp.num,sp.den))return fail('SPLIT_NOT_ALIGNED');
 const so=mulDiv(off,sp.num,sp.den),b=clone(c);
 b.id=newId;b.at=at;b.src_in=(c.src_in as number)+so;c.src_out=(c.src_in as number)+so;
 delete b.transition_in;
 if(c.audio){c.audio.fade_out=0;(b.audio as NonNullable<Clip['audio']>).fade_in=0;}
 (b.effects??[]).forEach((e,k)=>{e.id=`${newId}-e${k+1}`;});
 t.clips.splice(i+1,0,b);
 return{ok:true,doc};
}

/** FN-T-06 */
export function trimEdge(src:TimelineDocument,clipId:string,edge:'in'|'out',pos:number,sourceDuration:number):EditResult{
 const doc=clone(src),f=find(doc,clipId);if(!f)return fail('CLIP_NOT_FOUND');
 const {t,i,c}=f,FT=frameTicks(doc.video.fps);
 if(pos%FT!==0)return fail('TRIM_NOT_ALIGNED');
 const sp=speedOf(c);
 if(edge==='in'){
  const prevEnd=i===0?0:t.clips[i-1].at+clipDuration(t.clips[i-1]);
  if(pos<prevEnd)return fail('TRIM_OVERLAP');
  if(pos>=c.at+clipDuration(c))return fail('TRIM_EMPTY');
  const delta=pos-c.at;
  if(modNonZero(delta,sp.num,sp.den))return fail('TRIM_NOT_ALIGNED');
  const ns=(c.src_in as number)+mulDiv(delta,sp.num,sp.den);
  if(ns<0)return fail('TRIM_BEFORE_SOURCE');
  c.src_in=ns;c.at=pos;
 }else{
  if(pos<=c.at)return fail('TRIM_EMPTY');
  if(i+1<t.clips.length&&pos>t.clips[i+1].at)return fail('TRIM_OVERLAP');
  if(modNonZero(pos-c.at,sp.num,sp.den))return fail('TRIM_NOT_ALIGNED');
  const no=(c.src_in as number)+mulDiv(pos-c.at,sp.num,sp.den);
  if(no>sourceDuration)return fail('TRIM_BEYOND_SOURCE');
  c.src_out=no;
 }
 return{ok:true,doc};
}

/** FN-T-07 */
export function moveClip(src:TimelineDocument,clipId:string,newAt:number,trackId?:string):EditResult{
 const doc=clone(src),f=find(doc,clipId);if(!f)return fail('CLIP_NOT_FOUND');
 const {t,i,c}=f,FT=frameTicks(doc.video.fps);
 if(newAt<0||newAt%FT!==0)return fail('MOVE_NOT_ALIGNED');
 const tgt=trackId===undefined?t:doc.tracks.find(x=>x.id===trackId);
 if(!tgt)return fail('TRACK_NOT_FOUND');
 if(tgt.kind!==(c.source.kind==='audio'?'audio':'video'))return fail('MOVE_TRACK_KIND');
 t.clips.splice(i,1);
 const d=clipDuration(c);
 for(const o of tgt.clips)if(newAt<o.at+clipDuration(o)&&o.at<newAt+d)return fail('MOVE_OVERLAP');
 c.at=newAt;tgt.clips.push(c);tgt.clips.sort((a,b)=>a.at-b.at);
 return{ok:true,doc};
}

/** FN-T-08 */
export function deleteClip(src:TimelineDocument,clipId:string,ripple:boolean):EditResult{
 const doc=clone(src),f=find(doc,clipId);if(!f)return fail('CLIP_NOT_FOUND');
 const {t,i,c}=f,d=clipDuration(c);
 t.clips.splice(i,1);
 if(ripple)for(const o of t.clips)if(o.at>c.at)o.at-=d;
 return{ok:true,doc};
}

/** FN-T-09 */
export function setSpeed(src:TimelineDocument,clipId:string,num:number,den:number):EditResult{
 const doc=clone(src),f=find(doc,clipId);if(!f)return fail('CLIP_NOT_FOUND');
 const {t,i,c}=f,FT=frameTicks(doc.video.fps);
 if(c.source.kind==='photo')return fail('SPEED_PHOTO');
 if(!(num>0&&den>0)||num*4<den||num>4*den)return fail('SPEED_RANGE');
 const g=gcd(num,den);num/=g;den/=g;
 const raw=mulDiv((c.src_out as number)-(c.src_in as number),den,num),d=Math.floor(raw/FT)*FT;
 if(d<=0)return fail('SPEED_TOO_SHORT');
 if(i+1<t.clips.length&&c.at+d>t.clips[i+1].at)return fail('SPEED_OVERLAP');
 c.src_out=(c.src_in as number)+mulDiv(d,num,den);c.speed={num,den};
 return{ok:true,doc};
}
