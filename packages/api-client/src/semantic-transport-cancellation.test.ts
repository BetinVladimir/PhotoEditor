import {it,expect,vi} from 'vitest';
import type {ApiClient} from './index';
import {proposeSemanticIntent,type SemanticEditIntent} from './semantic-propose';
import {applySemanticReview} from './semantic-apply';
import {closeSemanticReview,readSemanticReviewStatus,readSemanticRunIntent} from './semantic-close';
import {listSemanticTargets} from './semantic';
import {materializeSemanticShownVersion} from './semantic-shown-version';
import {readSemanticAppliedPreview} from './semantic-applied-preview';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const intent:SemanticEditIntent={schema_version:1,kind:'local_exposure',reference:{asset_version_id:id(4),source:{artifact_id:id(5),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},reference_mask:{artifact_id:id(6),sha256:'c'.repeat(64)},target_snapshot_id:id(7),strength:.5,ev:0,preserve_persons:true,prompt:null};
const names=['propose','apply','close','status','run','targets','shown','applied-preview'] as const;
function call(name:typeof names[number],request:ApiClient['request'],signal:AbortSignal){
 const client={request};
 switch(name){
 case 'propose':return proposeSemanticIntent(client,id(1),intent,'stable-key',signal);
 case 'apply':return applySemanticReview(client,id(1),id(2),{expected_revision:'1',plan_sha:'a'.repeat(64),target_decisions:[{target_id:id(3),decision:'reject'}]},'stable-key',signal);
 case 'close':return closeSemanticReview(client,id(1),id(2),{expected_revision:'1',plan_sha:'a'.repeat(64)},'stable-key',signal);
 case 'status':return readSemanticReviewStatus(client,id(1),id(2),signal);
 case 'run':return readSemanticRunIntent(client,id(1),id(2),signal);
 case 'targets':return listSemanticTargets(client,id(1),id(2),{signal});
 case 'shown':return materializeSemanticShownVersion(client,id(1),id(3),signal);
 case 'applied-preview':return readSemanticAppliedPreview(client,id(1),id(3),signal);
 }
}
function response(name:typeof names[number]):unknown{
 switch(name){
 case 'propose':return {run_id:id(8),workflow_id:id(9),state:'queued'};
 case 'apply':return {workflow_id:id(9),revision:'2',remaining:0,batch_id:id(10),child:null};
 case 'close':return {run_id:id(8),workflow_id:id(9),revision:'2',state:'completed',outputs:[],error_code:null};
 case 'status':return {run_id:id(8),workflow_id:id(9),revision:'1',state:'queued',plan_sha:null,remaining:0,can_close:false};
 case 'run':return {run_id:id(2),workflow_id:id(9),intent_id:null,revision:'1',state:'queued'};
 case 'targets':return {items:[],next_cursor:null};
 case 'shown':return {asset_version_id:id(10),asset_id:id(11)};
 case 'applied-preview':return {schema_version:1,tenant_id:id(1),target_id:id(3),child_run_id:id(8),working_image:{artifact_id:id(10),sha256:'a'.repeat(64)},after:{artifact_id:id(11),sha256:'b'.repeat(64),url:'https://objects.example/a?versionId=v',expires_at:new Date(Date.now()+30000).toISOString(),width:100,height:100,media_type:'image/png'}};
 }
}
it.each(names)('%s does not start IO for an already aborted caller',async name=>{
 const request=vi.fn(),controller=new AbortController();controller.abort();
 await expect(call(name,request,controller.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).not.toHaveBeenCalled();
});
it.each(names)('%s refuses even a valid wire response after cancellation',async name=>{
 let resolve!:(value:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done;})),controller=new AbortController();
 const pending=call(name,request,controller.signal);controller.abort();resolve(response(name));
 await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
it('target list retains the original AbortSignal while caller replaces its query object',async()=>{
 let resolve!:(value:unknown)=>void;const request=vi.fn().mockImplementation(()=>new Promise(done=>{resolve=done;})),original=new AbortController(),replacement=new AbortController(),query={signal:original.signal};
 const pending=listSemanticTargets({request},id(1),id(2),query);query.signal=replacement.signal;original.abort();resolve({items:[],next_cursor:null});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});expect(request.mock.calls[0][1]).toEqual({signal:original.signal});
});
