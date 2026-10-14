// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const memory=vi.hoisted(()=>new Map<string,string>());
vi.mock('@react-native-async-storage/async-storage',()=>({default:{
 getItem:async(key:string)=>memory.get(key)??null,
 setItem:async(key:string,value:string)=>{memory.set(key,value);},
 removeItem:async(key:string)=>{memory.delete(key);},
}}));
import {EditorReflection} from './editor-reflection';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)};
const mask={mask:ref(13),protected:ref(14),target_pixel:ref(15),protected_pixel:ref(16)};
const prepared={schema_version:1 as const,snapshot_id:id(17),ordinal:0,
 params_proposal:{schema_version:1 as const,base,strength:.5,mask:mask.mask,protected:mask.protected,model_lock_sha:'d'.repeat(64),calibration:ref(19)},
 target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel};
afterEach(()=>{cleanup();memory.clear();});
it('selects exact manual pair and submits server-prepared reflection intent',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const prepareReflection=vi.fn(async(_project:string,_body:unknown,_key:string)=>prepared);
 const submitReflection=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={
  me:vi.fn(async()=>({id:id(30)})),v2Binding:vi.fn(async()=>base),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  reflectionMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  prepareReflection,submitReflection,
  get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
 } as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorReflection api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} onSubmitted={onSubmitted}/>);
 const radio=await screen.findByRole('radio',{name:'Mask pair 1'});
 await waitFor(()=>expect(radio.getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(radio);
 fireEvent.click(screen.getByRole('button',{name:'Review reflection request'}));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(prepareReflection.mock.calls[0][1]).toMatchObject({mask:mask.mask,protected:mask.protected,target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel});
 expect(prepareReflection.mock.calls[0][2]).toBe(submitReflection.mock.calls[0][2]);
});
it('recovers the same reflection intent and key after an ambiguous response',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30),key='reflection:recover';
 const storageKey='photo.reflection-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 memory.set(storageKey,JSON.stringify({schema_version:1,project,key,body:prepared,ticket:null}));
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const submitReflection=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>base),
  createRenderInputSnapshot:vi.fn(),reflectionMaskPairs:vi.fn(),prepareReflection:vi.fn(),
  submitReflection,get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id}))} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorReflection api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} onSubmitted={onSubmitted}/>);
 const button=await screen.findByRole('button',{name:'Retry submission'});
 await waitFor(()=>expect(button.getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(button);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(submitReflection.mock.calls[0][2]).toBe(key);
 expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
 expect(api.prepareReflection).not.toHaveBeenCalled();
});
it('refuses to replay a stored reflection for a changed live source binding',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30);
 const storageKey='photo.reflection-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 memory.set(storageKey,JSON.stringify({schema_version:1,project,key:'reflection:old',body:prepared,ticket:null}));
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>({...base,source:ref(99)})),
  createRenderInputSnapshot:vi.fn(),reflectionMaskPairs:vi.fn(),prepareReflection:vi.fn(),submitReflection:vi.fn()} as unknown as OwnerAPI;
 render(<EditorReflection api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} onSubmitted={vi.fn()}/>);
 expect((await screen.findByRole('alert')).textContent).toBe('Reflection mask choices are unavailable.');
 expect(api.submitReflection).not.toHaveBeenCalled();
 expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
