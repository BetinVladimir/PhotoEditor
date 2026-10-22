// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent,waitFor,cleanup} from '@testing-library/react';
import type {ApiClient} from '@photoeditor/api-client';
import en from '../../../locales/en/common.json';
const storage=vi.hoisted(()=>new Map<string,string>());
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>storage.get(k)??null,setItem:async(k:string,v:string)=>{storage.set(k,v)},removeItem:async(k:string)=>{storage.delete(k)},getAllKeys:async()=>[...storage.keys()]}}));
vi.mock('./semantic-attempt-store',()=>({semanticJournalLock:async(_origin:string,action:()=>Promise<unknown>)=>action()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>en[key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
import {CaptureSessionCreate} from './capture-session-create';
import {OwnerAPI} from './owner-api';
import {capturePolicySHA} from './capture-capabilities';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
afterEach(()=>{cleanup();storage.clear();});
describe('Capture creation rendered web flow',()=>{
 it('retains admission body/key after a lost response and reads policies from the V2 envelope',async()=>{
  const policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(5),sha256:'a'.repeat(64)}};
  const capture_policy={schema_version:1,checks:['focus','clipping'],max_queue_age_seconds:30,calibration:policy.calibration};
  const resource=(resourceID:string,name:string)=>({id:resourceID,revision:'1',document:null,data:{id:resourceID,revision:'1',resource_type:'shoot_sessions',tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T10:00:00Z',updated_at:'2026-10-17T10:00:00Z',consent_epoch:'1',project_id:id(2),collection_id:id(4),name,state:'open',policy,capture_policy}});
  let lost=true;const mutations:Array<{body:unknown;idempotencyKey:string}>=[];
  const request=vi.fn(async(path:string,options?:{method?:string;body?:unknown;idempotencyKey?:string})=>{
   if(path==='/api/v1/me')return{id:id(7),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};
   if(path.endsWith('/shoot-sessions/'+id(6)))return resource(id(6),'New studio');
   if(path.includes('/collections?'))return {items:[{id:id(4),tenant_id:'11111111-1111-4111-8111-111111111111',project_id:id(2),name:'Frames',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'}],next_cursor:null};
   if(path.endsWith('/shoot-sessions/'+id(3)))return resource(id(3),'Existing studio');
   if(path.includes('/capture-capabilities?'))return{schema_version:1,tenant_id:'11111111-1111-4111-8111-111111111111',project_id:id(2),template_id:id(3),template_revision:'1',policy_sha:capturePolicySHA({policy,capture_policy} as never),available:true,reason:null,expires_at:new Date(Date.now()+15000).toISOString()};
   if(options?.method==='POST'){
    mutations.push({body:options.body,idempotencyKey:options.idempotencyKey!});
    if(lost){lost=false;throw new TypeError('response lost after server commit')}
    return resource(id(6),'New studio');
   }
   throw new Error('Unexpected route');
  });
  const onCreated=vi.fn(),onBusy=vi.fn();
  render(<CaptureSessionCreate origin="https://capture.test" api={new OwnerAPI({request} as unknown as ApiClient,'11111111-1111-4111-8111-111111111111')} project={id(2)} sessions={[{id:id(3),name:'Existing studio'}]} onCreated={onCreated} onBusy={onBusy}/>);
  fireEvent.click(await screen.findByRole('radio',{name:'Frames'}));
  fireEvent.click(screen.getByRole('radio',{name:'Existing studio'}));
  fireEvent.change(screen.getByLabelText(en.capture_session_name),{target:{value:'New studio'}});
  fireEvent.click(screen.getByRole('button',{name:en.capture_create_session}));
  await screen.findByText(en.capture_create_error);
  expect(screen.queryByText(en.session_draft_discard)).toBeNull();
  expect(onCreated).not.toHaveBeenCalled();
  await waitFor(()=>expectEnabled(screen.getByRole('button',{name:en.capture_create_session})));
  await clickWhenEnabled(screen.getByRole('button',{name:en.capture_create_session}));
  await waitFor(()=>expect(onCreated).toHaveBeenCalledTimes(1));
  expect(mutations).toHaveLength(2);expect(mutations[1]).toEqual(mutations[0]);
  expect(mutations[0].body).toEqual({project_id:id(2),collection_id:id(4),name:'New studio',policy,capture_policy});
  expect(request.mock.calls.filter(([path])=>path.endsWith('/shoot-sessions/'+id(3)))).toHaveLength(1);
  expect(onCreated.mock.calls[0][0]).toEqual(resource(id(6),'New studio').data);
  await waitFor(()=>expect(onBusy.mock.calls.map(([value])=>value)).toEqual([true,false,true,false]));
 });
 it('refuses a template with no Capture policy before submitting a session',async()=>{
  const request=vi.fn(async(path:string)=>path==='/api/v1/me'?{id:id(7),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'}:path.includes('/collections?')?{items:[{id:id(4),tenant_id:'11111111-1111-4111-8111-111111111111',project_id:id(2),name:'Frames',description:'',state:'draft',revision:'1',sort_mode:'manual',created_at:'2026-10-17T10:00:00Z'}],next_cursor:null}:{id:id(3),revision:'1',document:null,data:{id:id(3),revision:'1',resource_type:'shoot_sessions',tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T10:00:00Z',updated_at:'2026-10-17T10:00:00Z',consent_epoch:'1',project_id:id(2),collection_id:id(4),name:'Ordinary session',state:'open',policy:{schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(5),sha256:'a'.repeat(64)}},capture_policy:null}});
  const onCreated=vi.fn();
  render(<CaptureSessionCreate origin="https://capture.test" api={new OwnerAPI({request} as unknown as ApiClient,'11111111-1111-4111-8111-111111111111')} project={id(2)} sessions={[{id:id(3),name:'Ordinary session'}]} onCreated={onCreated} onBusy={()=>{}}/>);
  fireEvent.click(await screen.findByRole('radio',{name:'Frames'}));
  fireEvent.click(screen.getByRole('radio',{name:'Ordinary session'}));
  expect(screen.getByRole('radio',{name:/Frames/}).getAttribute('aria-checked')).toBe('true');
  fireEvent.change(screen.getByLabelText(en.capture_session_name),{target:{value:'New studio'}});
  fireEvent.click(screen.getByRole('button',{name:en.capture_create_session}));
  await screen.findByText(en.capture_template_unavailable);
  expect(onCreated).not.toHaveBeenCalled();expect(request).toHaveBeenCalledTimes(3);expect((request.mock.calls as unknown as Array<[string,{method?:string}?]>).some(([,options])=>options?.method==='POST')).toBe(false);
 });

});
it('recovers a persisted unknown create on remount without template or worker discovery',async()=>{
 const {CaptureCreateAttemptStore}=await import('./capture-create-attempt-store'),storageAPI=(await import('@react-native-async-storage/async-storage')).default,tenant='11111111-1111-4111-8111-111111111111';
 const policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(5),sha256:'a'.repeat(64)}},capture_policy={schema_version:1,checks:['focus'],max_queue_age_seconds:30,calibration:policy.calibration},body={project_id:id(2),collection_id:id(4),name:'Recovered',policy,capture_policy} as const,key=id(8);
 const store=new CaptureCreateAttemptStore(storageAPI,'https://capture.test',id(7),tenant,id(2));await store.write({schema_version:1,key,request:body as never,ticket:null});
 const created={id:id(6),revision:'1',document:null,data:{id:id(6),revision:'1',resource_type:'shoot_sessions',tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T10:00:00Z',updated_at:'2026-10-17T10:00:00Z',consent_epoch:'1',state:'open',...body}};
 const request=vi.fn(async(path:string,options?:any)=>{if(path==='/api/v1/me')return{id:id(7),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};if(options?.method==='POST'){expect(options.idempotencyKey).toBe(key);expect(options.body).toEqual(body);return created;}if(path.endsWith('/shoot-sessions/'+id(6)))return created;throw new Error('unexpected worker/template/collection discovery');});
 const onCreated=vi.fn();render(<CaptureSessionCreate origin="https://capture.test" api={new OwnerAPI({request} as never,tenant)} project={id(2)} sessions={[]} onCreated={onCreated} onBusy={()=>{}}/>);const submit=await screen.findByRole('button',{name:en.capture_create_session});await waitFor(()=>expectEnabled(submit));await clickWhenEnabled(submit);await waitFor(()=>expect(onCreated).toHaveBeenCalledOnce());expect(await store.read()).toBeNull();
});
it('a known accepted capture receipt recovers using GET only',async()=>{
 const {CaptureCreateAttemptStore}=await import('./capture-create-attempt-store'),storageAPI=(await import('@react-native-async-storage/async-storage')).default,tenant='11111111-1111-4111-8111-111111111111';
 const policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(5),sha256:'a'.repeat(64)}},capture_policy={schema_version:1,checks:['focus'],max_queue_age_seconds:30,calibration:policy.calibration},body={project_id:id(2),collection_id:id(4),name:'Accepted',policy,capture_policy};
 const store=new CaptureCreateAttemptStore(storageAPI,'https://capture.test',id(7),tenant,id(2));await store.write({schema_version:1,key:id(8),request:body as never,ticket:{resource_id:id(6)}});const created={id:id(6),revision:'2',document:null,data:{id:id(6),revision:'2',resource_type:'shoot_sessions',tenant_id:'11111111-1111-4111-8111-111111111111',created_at:'2026-10-17T10:00:00Z',updated_at:'2026-10-17T10:00:00Z',consent_epoch:'1',state:'closed',...body}};
 const request=vi.fn(async(path:string,options?:any)=>{if(path==='/api/v1/me')return{id:id(7),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};if(options?.method==='POST')throw new Error('must not POST accepted receipt');if(path.endsWith('/shoot-sessions/'+id(6)))return created;throw new Error('unexpected discovery');});const onCreated=vi.fn();render(<CaptureSessionCreate origin="https://capture.test" api={new OwnerAPI({request} as never,tenant)} project={id(2)} sessions={[]} onCreated={onCreated} onBusy={()=>{}}/>);const submit=await screen.findByRole('button',{name:en.capture_create_session});await waitFor(()=>expectEnabled(submit));await clickWhenEnabled(submit);await waitFor(()=>expect(onCreated).toHaveBeenCalledOnce());expect(request.mock.calls.some(([,options])=>options?.method==='POST')).toBe(false);
});
it('offers same-key rejection discard only after a definitive server validation rejection',async()=>{
 const {CaptureCreateAttemptStore}=await import('./capture-create-attempt-store'),storageAPI=(await import('@react-native-async-storage/async-storage')).default,{APIError}=await import('@photoeditor/api-client'),tenant='11111111-1111-4111-8111-111111111111';
 const policy={schema_version:1,time_gap_seconds:60,neighbor_count:4,protect_manual_picks:true,camera_offsets:[],calibration:{artifact_id:id(5),sha256:'a'.repeat(64)}},capture_policy={schema_version:1,checks:['focus'],max_queue_age_seconds:30,calibration:policy.calibration},body={project_id:id(2),collection_id:id(4),name:'Rejected',policy,capture_policy};
 const store=new CaptureCreateAttemptStore(storageAPI,'https://capture.test',id(7),tenant,id(2));await store.write({schema_version:1,key:id(8),request:body as never,ticket:null});const request=vi.fn(async(path:string,options?:any)=>{if(path==='/api/v1/me')return{id:id(7),email:'fixture@example.test',locale:'en',theme:'system',timezone:'UTC',revision:'1'};if(options?.method==='POST')throw new APIError(422,'VALIDATION_FAILED');if(path.includes('/collections?'))return{items:[],next_cursor:null};throw new Error('unexpected discovery');});render(<CaptureSessionCreate origin="https://capture.test" api={new OwnerAPI({request} as never,tenant)} project={id(2)} sessions={[]} onCreated={()=>{}} onBusy={()=>{}}/>);const submit=await screen.findByRole('button',{name:en.capture_create_session});await waitFor(()=>expectEnabled(submit));await clickWhenEnabled(submit);const discard=await screen.findByRole('button',{name:en.session_draft_discard});expect((await store.read())?.key).toBe(id(8));fireEvent.click(discard);await waitFor(async()=>expect(await store.read()).toBeNull());expect(request.mock.calls.filter(([,options])=>options?.method==='POST')).toHaveLength(1);
});
