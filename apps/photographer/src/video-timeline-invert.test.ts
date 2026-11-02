import {expect,it} from 'vitest';
import type {TimelineDocument} from '@photoeditor/api-client';
import {canonicalize} from '@photoeditor/api-client';
import {loadVector} from './video-test-vectors';
import {applyCutsCommand,deleteCommand,invertCommand,moveCommand,speedCommand,splitCommand,trimCommand} from './video-timeline-invert';
type Case={id:string;op:string;args:Record<string,any>;expect:{doc?:unknown}};
const v=loadVector<{base:unknown;cases:Case[]}>('commands');
const b=(d:unknown)=>Buffer.from(canonicalize(d)).toString('utf8');
const D=(x:unknown)=>x as TimelineDocument;
it('invert(apply(doc)) restores the original for every successful vector',()=>{
 let n=0;const res:Record<string,TimelineDocument>={};
 for(const c of v.cases){
  if(!c.expect.doc)continue;
  const a=c.args,op=c.op.split(/[ (]/)[0];
  let base=D(v.base);
  if(c.id==='SP-06')base=(speedCommand('c2',2,1).apply(base) as any).doc;
  if(c.id==='AC-03')base=res['AC-01'];
  const cmd=op==='split_clip'?splitCommand(a.clip,a.at,a.new_id):op==='trim_edge'?trimCommand(a.clip,a.edge,a.pos,a.source_duration)
   :op==='move_clip'?moveCommand(a.clip,a.new_at,a.track):op==='delete_clip'?deleteCommand(a.clip,a.ripple)
   :op==='set_speed'?speedCommand(a.clip,a.num,a.den):applyCutsCommand(a.media_id,a.boundaries,a.accepted,a.mode,a.new_ids);
  const r=cmd.apply(base) as any;expect(r.ok,c.id).toBe(true);res[c.id]=r.doc;
  const back=invertCommand(base,cmd).apply(r.doc) as any;
  expect(b(back.doc),c.id).toBe(b(base));n++;
 }
 expect(n).toBe(v.cases.filter(c=>c.expect.doc).length);
});
