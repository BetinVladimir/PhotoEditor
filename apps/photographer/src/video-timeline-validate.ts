import type {Clip,TimelineDocument,ValidateContext,ValidationIssue,ValidationRule} from '@photoeditor/api-client';
import {canonicalize} from '@photoeditor/api-client';
import {clipDuration,TICKS_PER_SECOND} from './video-timeline-time';

export const SUPPORTED_FPS=['24/1','25/1','30/1','50/1','60/1','24000/1001','30000/1001','60000/1001'];
export const MAX_DOC_BYTES=4*1024*1024;
export const MAX_DURATION_TICKS=3*3600*TICKS_PER_SECOND;
const MAX_ISSUES=100,ID_RE=/^[A-Za-z0-9_-]{1,64}$/;
type J=Record<string,any>;
const isObj=(v:unknown):v is J=>typeof v==='object'&&v!==null&&!Array.isArray(v);
const isInt=(v:unknown)=>typeof v==='number'&&Number.isSafeInteger(v);
const num=(v:unknown)=>typeof v==='number'&&Number.isFinite(v);
const inR=(v:unknown,lo:number,hi:number)=>num(v)&&(v as number)>=lo&&(v as number)<=hi;
const vec=(v:unknown,n:number,lo:number,hi:number)=>Array.isArray(v)&&v.length===n&&v.every(x=>inR(x,lo,hi));

const KEYS:Record<string,string[]>={
 root:['schema_version','sequence_id','base_revision','timebase','video','audio','tracks','markers','master','rights'],
 timebase:['ticks_per_second'],video:['fps','width','height','color','background'],color:['space','range'],fps:['num','den'],
 audio:['sample_rate','channels'],track:['id','kind','locked','visible','muted','gain_db','clips'],
 clip:['id','source','at','src_in','src_out','duration','speed','fit','transform','motion','audio','transition_in','effects'],
 source:['kind','media_id','asset_version_id','sha256'],effect:['id','type','enabled','params','meta'],marker:['id','at','kind','label'],
 master:['effects','audio'],masterAudio:['loudness'],loudness:['target_lufs','true_peak_db'],rights:['music_declared','declared_at'],
 transition:['kind','duration','align'],clipAudio:['gain_db','fade_in','fade_out','mute'],transform:['scale','position','rotate','crop'],
 motion:['kind','from','to','easing'],pose:['scale','center'],
};
const BASIC:Record<string,[number,number]>={exposure_ev:[-5,5],temperature:[-100,100],tint:[-100,100],contrast:[-100,100],highlights:[-100,100],shadows:[-100,100],whites:[-100,100],blacks:[-100,100],saturation:[-100,100],vibrance:[-100,100]};

export function validateDocument(doc:TimelineDocument,ctx:ValidateContext):ValidationIssue[]{
 const out:ValidationIssue[]=[];
 const add=(rule:ValidationRule,pointer:string,code:string)=>{if(out.length<MAX_ISSUES)out.push({rule,pointer,code});};
 const d=doc as unknown as J,esc=(s:string)=>s.replace(/~/g,'~0').replace(/\//g,'~1');
 // VT-01
 const closed=(o:unknown,kind:string,p:string)=>{if(isObj(o))for(const k of Object.keys(o))if(!KEYS[kind].includes(k))add('VT-01',`${p}/${esc(k)}`,'DOC_SCHEMA');};
 closed(d,'root','');
 if(d.schema_version!==1)add('VT-01','/schema_version','DOC_SCHEMA');
 closed(d.timebase,'timebase','/timebase');closed(d.video,'video','/video');closed(d.video?.color,'color','/video/color');closed(d.video?.fps,'fps','/video/fps');
 closed(d.audio,'audio','/audio');closed(d.master,'master','/master');closed(d.master?.audio,'masterAudio','/master/audio');
 closed(d.master?.audio?.loudness,'loudness','/master/audio/loudness');closed(d.rights,'rights','/rights');
 const f=d.video?.fps;
 if(!isObj(f)||!SUPPORTED_FPS.includes(`${f.num}/${f.den}`))add('VT-01','/video/fps','DOC_SCHEMA');
 const tracks:J[]=Array.isArray(d.tracks)?d.tracks:[];
 tracks.forEach((t,ti)=>{
  closed(t,'track',`/tracks/${ti}`);
  (Array.isArray(t.clips)?t.clips:[]).forEach((c:J,ci:number)=>{
   const p=`/tracks/${ti}/clips/${ci}`;
   closed(c,'clip',p);closed(c.source,'source',p+'/source');closed(c.transition_in,'transition',p+'/transition_in');
   closed(c.audio,'clipAudio',p+'/audio');closed(c.transform,'transform',p+'/transform');closed(c.motion,'motion',p+'/motion');
   closed(c.motion?.from,'pose',p+'/motion/from');closed(c.motion?.to,'pose',p+'/motion/to');
   for(const k of ['at','src_in','src_out','duration'])if(k in c&&!isInt(c[k]))add('VT-01',`${p}/${k}`,'DOC_SCHEMA');
   (Array.isArray(c.effects)?c.effects:[]).forEach((e:J,ei:number)=>closed(e,'effect',`${p}/effects/${ei}`));
  });
 });
 (Array.isArray(d.markers)?d.markers:[]).forEach((m:J,mi:number)=>{closed(m,'marker',`/markers/${mi}`);if('at' in m&&!isInt(m.at))add('VT-01',`/markers/${mi}/at`,'DOC_SCHEMA');});
 if(out.length)return out;
 const clips=(t:J):Clip[]=>t.clips;
 const dur=(c:Clip)=>clipDuration(c);
 // VT-02
 const nv=tracks.filter(t=>t.kind==='video').length,na=tracks.filter(t=>t.kind==='audio').length;
 const nc=tracks.reduce((s,t)=>s+t.clips.length,0);
 let end=0;for(const t of tracks)for(const c of clips(t))end=Math.max(end,c.at+dur(c));
 if(canonicalize(doc).length>MAX_DOC_BYTES||nv>4||na>4||nc>2000||end>MAX_DURATION_TICKS)add('VT-02','/tracks','DOC_LIMIT');
 // VT-03
 const seen=new Set<string>();
 const chk=(id:unknown,p:string)=>{if(typeof id!=='string'||!ID_RE.test(id)||seen.has(id))add('VT-03',p,'DOC_ID_DUPLICATE');if(typeof id==='string')seen.add(id);};
 tracks.forEach((t,ti)=>{chk(t.id,`/tracks/${ti}/id`);clips(t).forEach((c,ci)=>{chk(c.id,`/tracks/${ti}/clips/${ci}/id`);c.effects.forEach((e,ei)=>chk(e.id,`/tracks/${ti}/clips/${ci}/effects/${ei}/id`));});});
 (d.markers as J[]).forEach((m,mi)=>chk(m.id,`/markers/${mi}/id`));
 // VT-04
 tracks.forEach((t,ti)=>{for(let ci=1;ci<t.clips.length;ci++){const a=t.clips[ci-1],b=t.clips[ci];if(b.at<a.at||a.at+dur(a)>b.at)add('VT-04',`/tracks/${ti}/clips/${ci}/at`,'CLIP_OVERLAP');}});
 // VT-05
 tracks.forEach((t,ti)=>clips(t).forEach((c,ci)=>{
  if(c.source.kind==='photo')return;
  const p=`/tracks/${ti}/clips/${ci}`,si=c.src_in as number,so=c.src_out as number;
  if(si<0)add('VT-05',p+'/src_in','CLIP_RANGE');
  else if(so<=si)add('VT-05',p+'/src_out','CLIP_RANGE');
  else{const sd=ctx.sourceDurations[(c.source as J).media_id];if(sd!==undefined&&so>sd)add('VT-05',p+'/src_out','CLIP_RANGE');}
 }));
 // VT-06
 tracks.forEach((t,ti)=>clips(t).forEach((c,ci)=>{
  const s=c.source as J,sid=s.media_id??s.asset_version_id,p=`/tracks/${ti}/clips/${ci}/source`;
  if(ctx.sourceStates[sid]!=='ready')add('VT-06',p,'SOURCE_NOT_READY');
  else if(ctx.sourceHashes[sid]!==s.sha256)add('VT-06',p,'SOURCE_HASH_MISMATCH');
 }));
 // VT-07
 const trackById=new Map(tracks.map(t=>[t.id as string,t]));
 const FT=Number((BigInt(TICKS_PER_SECOND)*BigInt(f.den))/BigInt(f.num));
 tracks.forEach((t,ti)=>clips(t).forEach((c,ci)=>{
  const p=`/tracks/${ti}/clips/${ci}`;
  const bad=(sub:string)=>add('VT-07',`${p}/${sub}`,'EFFECT_PARAM');
  if(c.fit!==undefined&&!['fit','fill','none'].includes(c.fit))bad('fit');
  const tf=c.transform;
  if(tf){if(!inR(tf.scale,0.1,8))bad('transform/scale');if(!vec(tf.position,2,-1,1))bad('transform/position');if(!inR(tf.rotate,-180,180))bad('transform/rotate');if(!vec(tf.crop,4,0,0.45))bad('transform/crop');}
  const au=c.audio;
  if(au){
   if(!inR(au.gain_db,-60,24))bad('audio/gain_db');
   for(const k of ['fade_in','fade_out'] as const)if(!isInt(au[k])||au[k]<0||au[k]%FT!==0)bad('audio/'+k);
   if(isInt(au.fade_in)&&isInt(au.fade_out)&&au.fade_in+au.fade_out>dur(c))bad('audio/fade_out');
   if(typeof au.mute!=='boolean')bad('audio/mute');
  }
  const m=c.motion;
  if(m){
   if(m.kind!=='pan_zoom')bad('motion/kind');
   if(!['linear','ease_in_out'].includes(m.easing))bad('motion/easing');
   for(const k of ['from','to'] as const)if(!isObj(m[k])||!inR(m[k].scale,1,4)||!vec(m[k].center,2,0,1))bad('motion/'+k);
  }
  if(c.source.kind==='photo'&&(!isInt(c.duration)||(c.duration as number)<FT||(c.duration as number)%FT!==0))bad('duration');
  c.effects.forEach((e,ei)=>{
   const ep=`${p}/effects/${ei}/params`,params=isObj(e.params)?e.params:{};
   const badp=(k:string)=>add('VT-07',`${ep}/${esc(k)}`,'EFFECT_PARAM');
   const curve=(k:string,v:unknown)=>{
    const ok=Array.isArray(v)&&v.length>=2&&v.length<=16&&v.every(q=>vec(q,2,0,1))&&v.every((q:number[],i:number)=>i===0||q[0]>(v as number[][])[i-1][0]);
    if(!ok)badp(k);
   };
   switch(e.type){
    case'color.basic':for(const[k,v]of Object.entries(params)){const r=BASIC[k];if(!r||!inR(v,r[0],r[1]))badp(k);}break;
    case'color.cdl':{
     const spec:Record<string,[number,number,number]>={slope:[3,0,4],offset:[3,-1,1],power:[3,0.1,4],saturation:[0,0,4]};
     for(const k of Object.keys(params))if(!(k in spec))badp(k);
     for(const[k,[n,lo,hi]]of Object.entries(spec)){if(!(k in params)){badp(k);continue;}if(n?!vec(params[k],n,lo,hi):!inR(params[k],lo,hi))badp(k);}
     break;}
    case'color.curves':{
     const ks=Object.keys(params);
     if(ks.length===0)badp('');
     for(const k of ks){if(!['master','r','g','b'].includes(k))badp(k);else curve(k,params[k]);}
     break;}
    case'color.lut3d':{
     const l=params.lut;
     if(!isObj(l)||l.kind!=='lut'||typeof l.media_id!=='string'||typeof l.sha256!=='string')badp('lut');
     if(!inR(params.intensity,0,1))badp('intensity');
     for(const k of Object.keys(params))if(k!=='lut'&&k!=='intensity')badp(k);
     break;}
    case'audio.gain':if(!inR(params.gain_db,-60,24))badp('gain_db');for(const k of Object.keys(params))if(k!=='gain_db')badp(k);break;
    case'audio.fade':for(const k of ['fade_in','fade_out'])if(!isInt(params[k])||(params[k] as number)<0)badp(k);for(const k of Object.keys(params))if(k!=='fade_in'&&k!=='fade_out')badp(k);break;
    case'audio.duck':{
     const sc=params.sidechain_tracks;
     if(!Array.isArray(sc)||sc.length<1||sc.length>4||new Set(sc).size!==sc.length)badp('sidechain_tracks');
     if(!inR(params.threshold_db,-60,0))badp('threshold_db');if(!inR(params.depth_db,0,40))badp('depth_db');
     if(!inR(params.attack_ms,1,1000))badp('attack_ms');if(!inR(params.release_ms,10,5000))badp('release_ms');
     for(const k of Object.keys(params))if(!['sidechain_tracks','threshold_db','depth_db','attack_ms','release_ms'].includes(k))badp(k);
     break;}
   }
  });
 }));
 // VT-08
 tracks.forEach((t,ti)=>clips(t).forEach((c,ci)=>{
  const tr=c.transition_in;if(!tr)return;
  const p=`/tracks/${ti}/clips/${ci}/transition_in`;
  if(ci===0&&tr.kind!=='fade_in'&&tr.kind!=='cut'){add('VT-08',p+'/kind','TRANSITION_INVALID');return;}
  const lim=ci===0?Math.floor(dur(c)/2):Math.floor(Math.min(dur(c),dur(t.clips[ci-1]))/2);
  if((tr.duration??0)>lim)add('VT-08',p+'/duration','TRANSITION_INVALID');
 }));
 // VT-09
 tracks.forEach((t,ti)=>clips(t).forEach((c,ci)=>c.effects.forEach((e,ei)=>{
  if(e.type!=='audio.duck')return;
  const sc=(e.params as J).sidechain_tracks;
  if(!Array.isArray(sc))return;
  const bad=sc.some((s:string)=>{const o=trackById.get(s);return !o||o.kind!=='audio'||s===t.id;});
  if(bad)add('VT-09',`/tracks/${ti}/clips/${ci}/effects/${ei}/params/sidechain_tracks`,'SIDECHAIN_INVALID');
 })));
 // VT-10
 if(ctx.forRender&&ctx.musicClipIds.length>0&&d.rights.music_declared!==true)add('VT-10','/rights/music_declared','RIGHTS_REQUIRED');
 return out;
}
