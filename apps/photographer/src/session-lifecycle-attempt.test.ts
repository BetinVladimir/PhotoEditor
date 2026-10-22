import {expect,it,vi} from 'vitest';
import {persistSessionLifecycleAttempt} from './session-lifecycle-attempt';
import type {ShootSession} from './capture-types';
import {SessionPolicyDraftStore,type SessionPolicyDraft} from './session-policy-draft';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const session:ShootSession={id:id(1),project_id:id(2),collection_id:id(3),name:'Shoot',revision:'3',state:'open',capture_policy:null,policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(4),sha256:'a'.repeat(64)}}};
const request={expected_revision:'3',name:session.name,state:'closed' as const,policy:session.policy,capture_policy:null};
it('persists lifecycle request/key and reuses exact uncertain attempt',async()=>{
 let saved:SessionPolicyDraft|null=null;
 const reserve=vi.fn(async(value:SessionPolicyDraft)=>{if(saved){if(saved.attempt?.key!==value.attempt?.key)throw new Error('CONFLICT');return;}saved=structuredClone(value)});const store={read:async()=>saved,reserveLifecycle:reserve};
 await persistSessionLifecycleAttempt(store,session,request,id(5));
 expect(saved).toMatchObject({uncertain:true,attempt:{key:id(5),request}});
 await persistSessionLifecycleAttempt(store,session,request,id(5));expect(reserve).toHaveBeenCalledTimes(2);
 await expect(persistSessionLifecycleAttempt(store,session,request,id(6))).rejects.toThrow('CONFLICT');
});
it('refuses to overwrite an existing policy draft',async()=>{
 const draft:SessionPolicyDraft={schema_version:1,session_id:session.id,base_revision:session.revision,name:session.name,policy:session.policy,saved_at:Date.now()};
 const store={read:async()=>draft,reserveLifecycle:vi.fn(async()=>{throw new Error('DRAFT_CONFLICT')})};
 await expect(persistSessionLifecycleAttempt(store,session,request,id(5))).rejects.toThrow('DRAFT_CONFLICT');
 expect(store.reserveLifecycle).toHaveBeenCalledTimes(1);
});

it('serializes competing lifecycle reservations in the real store',async()=>{
 const data=new Map<string,string>();const store=new SessionPolicyDraftStore({getItem:async k=>data.get(k)??null,setItem:async(k,v)=>{data.set(k,v)},removeItem:async k=>{data.delete(k)}},'lifecycle-race',id(7),id(8),session.id);
 const results=await Promise.allSettled([persistSessionLifecycleAttempt(store,session,request,id(5)),persistSessionLifecycleAttempt(store,session,request,id(6))]);
 expect(results.filter(result=>result.status==='fulfilled')).toHaveLength(1);
 expect((await store.read())?.attempt?.key).toBe(id(5));
});
