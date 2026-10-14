import {describe,it,expect} from 'vitest';
import {readPerspectiveProposal,type PerspectiveProposal} from './perspective-proposal';
import {OwnerAPI} from './owner-api';
import type {ApiClient} from '@photoeditor/api-client';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function fixture():PerspectiveProposal{return {schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'perspective:canvas',source_sha256:'c'.repeat(64)},status:'review',requires_review:true};}
const read=(v:unknown)=>readPerspectiveProposal(v,id(1),id(2),id(3),fixture().base);
describe('perspective adopted proposal metadata',()=>{
 it('owns the descriptor and allows a new canvas',()=>{const v=fixture(),owned=read(v);v.canvas.width=2;v.base.source.sha256='d'.repeat(64);expect(owned.canvas.width).toBe(100);expect(owned.base.source.sha256).toBe('a'.repeat(64));});
 it('requires exact request scope and current source lineage binding',()=>{
  for(const change of [{tenant_id:id(11)},{project_id:id(11)},{producer_task_id:id(11)},{base:{...fixture().base,canvas_hash:'d'.repeat(64)}},{base:{...fixture().base,source:ref(11)}}])expect(()=>read({...fixture(),...change})).toThrow();
 });
 it('refuses review downgrade, role aliases and invalid geometry',()=>{
  for(const change of [{requires_review:false},{status:'pass',requires_review:false},{report:ref(9)},{result:ref(7)},{canvas:{...fixture().canvas,orientation:8}},{canvas:{...fixture().canvas,width:NaN}},{canvas:{...fixture().canvas,width:200_000_001}},{canvas:{...fixture().canvas,object_key:'injected'}},{url:'untrusted'}])expect(()=>read({...fixture(),...change})).toThrow();
 });
 it('accepts identity only with exact source and a pass report',()=>{
  const v=fixture();v.working_image={...v.base.source};v.status='pass';v.requires_review=false;expect(read(v).status).toBe('pass');
  v.working_image.sha256='d'.repeat(64);expect(()=>read(v)).toThrow();
 });
 it('freezes request scope before asynchronous transport and propagates cancellation',async()=>{
  const v=fixture(),base=structuredClone(v.base),controller=new AbortController();let finish!:(v:unknown)=>void;let path='',options:unknown;
  const client={request:(p:string,o:unknown)=>{path=p;options=o;return new Promise(resolve=>{finish=resolve;});}} as unknown as ApiClient;
  const pending=new OwnerAPI(client,id(1)).perspectiveProposal(id(2),id(3),base,controller.signal);base.source.sha256='d'.repeat(64);finish(v);
  expect((await pending).base.source.sha256).toBe('a'.repeat(64));expect(path).toContain(`/versions/${id(6)}/perspective-proposals/${id(3)}`);expect(options).toEqual({method:'GET',signal:controller.signal});
 });
 it('rejects malformed scope before network IO',async()=>{
  let calls=0;const client={request:()=>{calls++;}} as unknown as ApiClient;
  await expect(new OwnerAPI(client,id(1)).perspectiveProposal('../foreign',id(3),fixture().base)).rejects.toThrow();expect(calls).toBe(0);
 });
});
