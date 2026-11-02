import {expect,it} from 'vitest';
import type {TimelineDocument} from '@photoeditor/api-client';
import {canonicalize} from '@photoeditor/api-client';
import {loadVector} from './video-test-vectors';
import {applyCuts} from './video-timeline-cuts';
import {deleteClip,moveClip,setSpeed,splitClip,trimEdge} from './video-timeline-commands';

type Case={id:string;op:string;args:Record<string,any>;expect:{error?:string;doc?:unknown;applied?:unknown[]}};
const main=loadVector<{base:unknown;cases:Case[];source_duration_ticks:Record<string,number>}>('commands');
const extra=loadVector<{base:unknown;cases:Case[]}>('commands_extra');
const bytes=(d:unknown)=>Buffer.from(canonicalize(d)).toString('utf8');
const D=(x:unknown)=>x as TimelineDocument;
const ok=(r:any)=>{if(!r.ok)throw new Error(r.error);return r.doc as TimelineDocument;};

function run(c:Case,doc:TimelineDocument):any{
 const a=c.args,op=(c.op??'apply_cuts').split(/[ (]/)[0];
 switch(op){
  case'split_clip':return splitClip(doc,a.clip,a.at,a.new_id);
  case'trim_edge':return trimEdge(doc,a.clip,a.edge,a.pos,a.source_duration);
  case'move_clip':return moveClip(doc,a.clip,a.new_at,a.track);
  case'delete_clip':return deleteClip(doc,a.clip,a.ripple);
  case'set_speed':return setSpeed(doc,a.clip,a.num,a.den);
  case'apply_cuts':return applyCuts(doc,a.media_id,a.boundaries,a.accepted,a.mode,a.new_ids);
 }
 throw new Error('unknown op '+c.op);
}
function check(c:Case,base:TimelineDocument,res:any){
 const before=bytes(base);
 if(c.expect.error){expect(res.ok,c.id).toBe(false);expect(res.error,c.id).toBe(c.expect.error);}
 else{expect(res.ok,c.id).toBe(true);expect(bytes(res.doc),c.id).toBe(bytes(c.expect.doc));
  if(c.expect.applied)expect(res.applied,c.id).toEqual(c.expect.applied);}
 expect(bytes(base),c.id+' input mutated').toBe(before);
}
it('commands.json: 28 vectors incl. applyCuts, inputs never mutated',()=>{
 expect(main.cases.length).toBe(28);
 const results:Record<string,TimelineDocument>={};
 for(const c of main.cases){
  let base=D(main.base);
  if(c.id==='SP-06')base=ok(setSpeed(D(main.base),'c2',2,1));
  if(c.id==='AC-03')base=results['AC-01'];
  const res=run(c,base);check(c,base,res);if(res.ok)results[c.id]=res.doc;
 }
});
it('commands_extra.json: markers_only and id exhaustion',()=>{
 expect(extra.cases.length).toBe(4);
 const res:Record<string,TimelineDocument>={};
 for(const c of extra.cases){
  const base=c.id==='TC-T-AC-M2'?res['TC-T-AC-M1']:D(extra.base);
  const r=run(c,base);check(c,base,r);if(r.ok)res[c.id]=r.doc;
 }
});
it('applyCuts repeat on the same report does not consume ids',()=>{
 const c=main.cases.find(x=>x.id==='AC-01') as Case;
 const first=run(c,D(main.base));
 const again=applyCuts(first.doc,c.args.media_id,c.args.boundaries,c.args.accepted,c.args.mode,['z1']);
 expect(again.ok&&again.applied).toEqual([]);
});
