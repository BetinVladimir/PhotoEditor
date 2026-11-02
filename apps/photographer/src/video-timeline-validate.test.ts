import {expect,it} from 'vitest';
import type {TimelineDocument,ValidateContext} from '@photoeditor/api-client';
import {loadVector} from './video-test-vectors';
import {validateDocument} from './video-timeline-validate';
type Patch={op:'set'|'append';path:string;value:unknown};
type Case={id:string;desc:string;patches:Patch[];ctx_patch:Record<string,unknown>;expect:{rule:string;pointer:string;code:string}[]};
const v=loadVector<{base_doc:unknown;base_ctx:unknown;cases:Case[]}>('validate_cases');
const walk=(root:any,path:string)=>path.split('/').filter(Boolean).reduce((o,k)=>o[Array.isArray(o)?Number(k):k],root);
it('TC-T-VAL-00…20 match the shared vector (rule, pointer, code, order)',()=>{
 expect(v.cases.length).toBe(21);
 for(const c of v.cases){
  const doc=JSON.parse(JSON.stringify(v.base_doc)),ctx=JSON.parse(JSON.stringify(v.base_ctx));
  for(const p of c.patches){
   const parts=p.path.split('/').filter(Boolean),last=parts.pop() as string,parent=walk(doc,'/'+parts.join('/'));
   if(p.op==='append')walk(doc,p.path).push(p.value);else if(Array.isArray(parent))parent[Number(last)]=p.value;else parent[last]=p.value;
  }
  Object.assign(ctx,c.ctx_patch);
  expect(validateDocument(doc as TimelineDocument,ctx as ValidateContext),c.id).toEqual(c.expect);
 }
});
it('does not mutate the document and caps issues at 100',()=>{
 const doc=JSON.parse(JSON.stringify(v.base_doc));
 doc.tracks[0].clips=Array.from({length:150},(_,i)=>({...doc.tracks[0].clips[0],id:'x'+i,at:0}));
 const before=JSON.stringify(doc);
 expect(validateDocument(doc,v.base_ctx as ValidateContext).length).toBeLessThanOrEqual(100);
 expect(JSON.stringify(doc)).toBe(before);
});
it('rejects out-of-range effect and clip properties from 23 §11',()=>{
 const doc=JSON.parse(JSON.stringify(v.base_doc));
 doc.tracks[0].clips[0].transform={scale:9,position:[0,0],rotate:0,crop:[0,0,0,0]};
 doc.tracks[0].clips[0].effects=[{id:'e1',type:'color.cdl',enabled:true,params:{slope:[1,1,1],offset:[0,0,0],power:[1,1,1],saturation:5}}];
 const r=validateDocument(doc,v.base_ctx as ValidateContext).map(i=>i.pointer);
 expect(r).toEqual(['/tracks/0/clips/0/transform/scale','/tracks/0/clips/0/effects/0/params/saturation']);
});
