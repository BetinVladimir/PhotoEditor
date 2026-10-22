import {describe,it,expect} from 'vitest';
import {readLensProposal,type LensProposal} from './lens-proposal';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
function proposal():LensProposal{return {schema_version:1,tenant_id:id(1),project_id:id(2),producer_task_id:id(3),result_id:id(4),result:ref(5),base:{asset_version_id:id(6),source:ref(7),canvas_hash:'b'.repeat(64)},working_image:ref(8),changed_mask:ref(9),report:ref(10),canvas:{width:100,height:80,orientation:1,space_id:'lens:canvas',source_sha256:'c'.repeat(64)},status:'pass',requires_review:false};}
const read=(v:LensProposal)=>readLensProposal(v,id(1),id(2),id(3),proposal().base);
describe('qualified Lens proposal',()=>{
 it('accepts both covered pass and explicitly flagged uncovered review results',()=>{const value=proposal();expect(read(value).status).toBe('pass');value.status='review';value.requires_review=true;expect(read(value).requires_review).toBe(true);});
 it('owns exact source and root descriptors',()=>{const value=proposal(),owned=read(value);value.result.sha256='b'.repeat(64);expect(owned.result.sha256).toBe('a'.repeat(64));const changed=proposal();changed.base.source.sha256='c'.repeat(64);expect(()=>read(changed)).toThrow();});
 it('rejects inconsistent review acknowledgement and aliased changed-mask evidence',()=>{const value=proposal();value.requires_review=true;expect(()=>read(value)).toThrow();const alias=proposal();alias.changed_mask=alias.working_image;expect(()=>read(alias)).toThrow();});
 it('accepts exact zero-strength bypass but refuses modified source bytes',()=>{const value=proposal();value.working_image=value.base.source;expect(read(value).working_image).toEqual(value.base.source);value.working_image={...value.base.source,sha256:'b'.repeat(64)};expect(()=>read(value)).toThrow();});
});
