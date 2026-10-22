import {it,expect} from 'vitest';
import {SessionPolicyPersistence} from './session-policy-persistence';
import {SessionPolicyDraftStore} from './session-policy-draft';
import type {ShootSession} from './capture-types';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const session={id:id(1),revision:'3',name:'Shoot',project_id:id(5),collection_id:id(6),state:'open',capture_policy:null,policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(2),sha256:'a'.repeat(64)}}} as ShootSession;
const local={name:'',gap:'-',neighbors:'',protect:true,offsets:[],cameraId:'partial',cameraSeconds:'-'};
function setup(){const data=new Map<string,string>();const store=new SessionPolicyDraftStore({getItem:async k=>data.get(k)??null,setItem:async(k,v)=>{data.set(k,v)},removeItem:async k=>{data.delete(k)}},'persistence-origin',id(3),id(4),id(1));return {data,store,p:new SessionPolicyPersistence(store,session)};}
it('preserves partial input across a reload and refuses edits before explicit restore',async()=>{const {p,store}=setup();await expect(p.changed(local)).rejects.toThrow('NOT_READY');expect(await p.load()).toBeNull();await p.changed(local);const next=new SessionPolicyPersistence(store,session),draft=await next.load();await expect(next.changed(null)).rejects.toThrow('NOT_READY');const restored=next.restore();restored.gap='120';expect(draft?.local?.gap).toBe('-');await next.changed(null);expect(await store.read()).toBeNull();});
it('requires comparison when the server revision changed and supports explicit discard',async()=>{const {p,store}=setup();await p.load();await p.changed(local);const next=new SessionPolicyPersistence(store,{...session,revision:'4'}),draft=await next.load();expect(()=>next.restore()).toThrow('REQUIRES_COMPARISON');expect(await store.read()).not.toBeNull();await next.discard();await next.changed({...local,gap:'90'});expect((await store.read())?.base_revision).toBe('4');});
it('ignores edits after scope disposal without erasing the stored draft',async()=>{const {p,store}=setup();await p.load();await p.changed(local);p.close();await p.changed(null);expect((await store.read())?.local).toEqual(local);});

it('persists uncertain admission before transport and forbids raw-field restoration after restart',async()=>{const {p,store}=setup();await p.load();await p.changed(local);await p.markUncertain();expect((await store.read())?.uncertain).toBe(true);const next=new SessionPolicyPersistence(store,session);await next.load();expect(()=>next.restore()).toThrow('REQUIRES_COMPARISON');});

it('durably owns exact request/key and refuses a different attempt',async()=>{const {p,store}=setup();await p.load();await p.changed(local);const request={expected_revision:'3',name:'Updated',state:'open' as const,policy:structuredClone(session.policy),capture_policy:null},key=id(9);await p.prepare(request,key);request.name='caller mutation';const saved=await store.read();expect(saved?.attempt?.request.name).toBe('Updated');expect(saved?.attempt?.key).toBe(key);await expect(p.prepare(request,id(10))).rejects.toThrow('ATTEMPT_CHANGED');});

it('clears confirmed attempt after disposal while retaining a newer attempt under the same scope',async()=>{const {p,store}=setup();await p.load();await p.changed(local);const request={expected_revision:'3',name:'Updated',state:'open' as const,policy:session.policy,capture_policy:null};await p.prepare(request,id(9));p.close();expect(await p.complete(id(9))).toBe(true);expect(await store.read()).toBeNull();const next=new SessionPolicyPersistence(store,session);await next.load();await next.changed({...local,gap:'120'});await next.prepare(request,id(10));expect(await p.complete(id(9))).toBe(false);expect((await store.read())?.attempt?.key).toBe(id(10));});

it('explicit comparison preserves untouched current fields and retains partial local changes without writing',async()=>{const {p,store}=setup();await p.load();await p.changed({...local,name:session.name,neighbors:String(session.policy.neighbor_count),gap:'-',cameraId:'partial'});const current={...session,revision:'4',name:'Server rename',policy:{...session.policy,neighbor_count:8,protect_manual_picks:false,camera_offsets:[{camera_hash:'b'.repeat(64),seconds:10}]}};const next=new SessionPolicyPersistence(store,current);await next.load();const merged=next.restoreCompared();expect(merged).toMatchObject({name:'Server rename',neighbors:'8',protect:false,gap:'-',cameraId:'partial',offsets:[{camera_hash:'b'.repeat(64),seconds:'10'}]});expect((await store.read())?.base_revision).toBe('3');await next.changed(merged);expect((await store.read())?.base_revision).toBe('4');});

it('rejects invalid attempts before pinning so corrected input can be admitted',async()=>{const {p,store}=setup();await p.load();await p.changed(local);const request={expected_revision:'3',name:'Updated',state:'open' as const,policy:session.policy,capture_policy:null};await expect(p.prepare(request,'invalid')).rejects.toThrow('ATTEMPT_INVALID');expect((await store.read())?.attempt).toBeUndefined();await p.prepare(request,id(9));expect((await store.read())?.attempt?.key).toBe(id(9));});

it('rejects stale draft writes over an uncertain lifecycle attempt',async()=>{
 const {store}=setup(),request={expected_revision:'3',name:session.name,state:'closed' as const,policy:session.policy,capture_policy:null};
 const record={schema_version:1 as const,session_id:session.id,base_revision:'3',name:session.name,policy:session.policy,uncertain:true,attempt:{key:id(9),request},saved_at:Date.now()};
 await store.reserveLifecycle(record);
 const stale={...record,uncertain:false};delete (stale as Partial<typeof record>).attempt;
 await expect(store.write(stale)).rejects.toThrow('ATTEMPT_CHANGED');
 expect((await store.read())?.attempt?.key).toBe(id(9));
 await store.write(stale,id(9));expect((await store.read())?.attempt).toBeUndefined();
});

it('does not let stale field cleanup delete a reserved lifecycle request',async()=>{
 const {store}=setup();const request={expected_revision:'3',name:session.name,state:'closed' as const,policy:session.policy,capture_policy:null};
 await store.reserveLifecycle({schema_version:1,session_id:session.id,base_revision:'3',name:session.name,policy:session.policy,uncertain:true,attempt:{key:id(9),request},saved_at:Date.now()});
 await expect(store.clear()).rejects.toThrow('ATTEMPT_CHANGED');
 expect((await store.read())?.attempt?.key).toBe(id(9));
 await store.clear(id(9));expect(await store.read()).toBeNull();
});
