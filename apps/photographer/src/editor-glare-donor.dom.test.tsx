// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const memory=vi.hoisted(()=>new Map<string,string>());
vi.mock('@react-native-async-storage/async-storage',()=>({default:{
 getItem:async(key:string)=>memory.get(key)??null,
 setItem:async(key:string,value:string)=>{memory.set(key,value);},
 removeItem:async(key:string)=>{memory.delete(key);},
}}));
import {EditorGlareDonor} from './editor-glare-donor';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)};
const donor={asset_version_id:id(3),source:ref(4),canvas_hash:'c'.repeat(64)};
const session={id:id(5),project_id:id(6),collection_id:id(7),name:'Portrait shoot',state:'closed',revision:'1',policy:{} as never,capture_policy:null};
const source={observation_id:id(8),source_faces:ref(9)};
const candidate={observation_id:id(10),asset_id:id(11),asset_version_id:donor.asset_version_id,source:donor.source,donor_faces:ref(12)};
const mask={glasses_mask:ref(13),protected:ref(14),target_pixel:ref(15),protected_pixel:ref(16)};
const prepared={schema_version:1 as const,shoot_session_id:session.id,snapshot_id:id(17),ordinal:0,source_observation_id:source.observation_id,donor_observation_id:candidate.observation_id,
 params_proposal:{schema_version:1 as const,mode:'donor' as const,base,donor,strength:.5,glasses_mask:mask.glasses_mask,protected:mask.protected,confirmation:ref(18),source_faces:source.source_faces,donor_faces:candidate.donor_faces,model_lock_sha:'d'.repeat(64),calibration:ref(19)}};
afterEach(()=>{cleanup();memory.clear();});
it('selects exact producer refs and submits the server-prepared donor intent',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const prepareGlareDonor=vi.fn(async(_project:string,_body:unknown,_key:string)=>prepared);
 const submitGlareDonor=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={
  me:vi.fn(async()=>({id:id(30)})),
  v2Binding:vi.fn(async(asset:string)=>asset===id(31)?base:donor),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  shootSessions:vi.fn(async()=>({items:[session],next_cursor:null})),
  glareSourceCandidates:vi.fn(async()=>({items:[source],next_cursor:null})),
  glareMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  glareDonorCandidates:vi.fn(async()=>({source_faces:source.source_faces,items:[candidate],next_cursor:null})),
  prepareGlareDonor,submitGlareDonor,
  get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
 } as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorGlareDonor api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} collection={session.collection_id} onSubmitted={onSubmitted}/>);
 async function choose(name:string){await screen.findByText(name);const radio=screen.getByRole('radio',{name});await waitFor(()=>expectEnabled(radio));await clickWhenEnabled(radio);}
 await choose('Shoot 1: Portrait shoot');await choose('Source face 1');await choose('Donor frame 1');await choose('Mask pair 1');
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Review donor glare request'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Review donor glare request'}));
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 const selection=prepareGlareDonor.mock.calls[0][1] as Record<string,unknown>;
 expect(selection).toMatchObject({source_observation_id:source.observation_id,donor_observation_id:candidate.observation_id,
  glasses_mask:mask.glasses_mask,protected:mask.protected,target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel});
 expect(prepareGlareDonor.mock.calls[0][2]).toBe(submitGlareDonor.mock.calls[0][2]);
});

it('retains but does not replay saved glare intent after the live source changes',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30);
 const storageKey='photo.glare-donor-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 const record={schema_version:1,project,key:'glare:recovery',body:prepared,ticket:null};
 memory.set(storageKey,JSON.stringify(record));
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>({...base,source:{...base.source,sha256:'e'.repeat(64)}})),
  createRenderInputSnapshot:vi.fn(),shootSessions:vi.fn(),prepareGlareDonor:vi.fn(),submitGlareDonor:vi.fn()} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();
 render(<EditorGlareDonor api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} onSubmitted={onSubmitted}/>);
 await screen.findByRole('alert');
 expect(screen.queryByRole('button',{name:'Retry submission'})).toBeNull();
 expect(api.submitGlareDonor).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
 expect(JSON.parse(memory.get(storageKey)!)).toEqual(record);
});

it('blocks glare-donor POST when capability is withdrawn during preparation without rerender',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 let available=true;let finish!:(value:typeof prepared)=>void;
 const prepareGlareDonor=vi.fn((_project:string,_body:unknown,_key:string)=>new Promise<typeof prepared>(resolve=>{finish=resolve;}));
 const submitGlareDonor=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={
  me:vi.fn(async()=>({id:id(30)})),
  v2Binding:vi.fn(async(asset:string)=>asset===id(31)?base:donor),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  shootSessions:vi.fn(async()=>({items:[session],next_cursor:null})),
  glareSourceCandidates:vi.fn(async()=>({items:[source],next_cursor:null})),
  glareMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  glareDonorCandidates:vi.fn(async()=>({source_faces:source.source_faces,items:[candidate],next_cursor:null})),
  prepareGlareDonor,submitGlareDonor,
  get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
 } as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorGlareDonor operationAvailable={()=>available} api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} collection={session.collection_id} onSubmitted={onSubmitted}/>);
 async function choose(name:string){await screen.findByText(name);const radio=screen.getByRole('radio',{name});await waitFor(()=>expectEnabled(radio));await clickWhenEnabled(radio);}
 await choose('Shoot 1: Portrait shoot');await choose('Source face 1');await choose('Donor frame 1');await choose('Mask pair 1');
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Review donor glare request'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Review donor glare request'}));
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(prepareGlareDonor).toHaveBeenCalledTimes(1));
 available=false;await act(async()=>finish(prepared));
 expect(submitGlareDonor).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});

it('opens accepted glare-donor after worker capability disappears without submitting',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30);
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const storageKey='photo.glare-donor-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 memory.set(storageKey,JSON.stringify({schema_version:1,project,key:'accepted:recovery',body:prepared,ticket}));
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>base),get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
  createRenderInputSnapshot:vi.fn(),shootSessions:vi.fn(),prepareGlareDonor:vi.fn(),submitGlareDonor:vi.fn()} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();
 render(<EditorGlareDonor api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} operationAvailable={()=>false} onSubmitted={onSubmitted}/>);
 const open=await screen.findByRole('button',{name:'Open result'});
 await waitFor(()=>expectEnabled(open));await clickWhenEnabled(open);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.submitGlareDonor).not.toHaveBeenCalled();expect(api.prepareGlareDonor).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});
