// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
const memory=vi.hoisted(()=>new Map<string,string>());
vi.mock('@react-native-async-storage/async-storage',()=>({default:{
 getItem:async(key:string)=>memory.get(key)??null,
 setItem:async(key:string,value:string)=>{memory.set(key,value);},
 removeItem:async(key:string)=>{memory.delete(key);},
}}));
import {EditorOpenEyes} from './editor-open-eyes';
import type {OwnerAPI} from './owner-api';
import {OpenEyesAttemptStore} from './open-eyes-attempt-store';
import AsyncStorage from '@react-native-async-storage/async-storage';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)};
const donor={asset_version_id:id(3),source:ref(4),canvas_hash:'c'.repeat(64)};
const session={id:id(5),project_id:id(6),collection_id:id(7),name:'Portrait shoot',state:'closed',revision:'1',policy:{} as never,capture_policy:null};
const source={observation_id:id(8),source_faces:ref(9)};
const candidate={observation_id:id(10),asset_id:id(11),asset_version_id:donor.asset_version_id,source:donor.source,donor_faces:ref(12)};
const mask={glasses_mask:ref(13),protected:ref(14),target_pixel:ref(15),protected_pixel:ref(16)};
const prepared={schema_version:1 as const,shoot_session_id:session.id,snapshot_id:id(17),ordinal:0,source_observation_id:source.observation_id,donor_observation_id:candidate.observation_id,
 params_proposal:{schema_version:1 as const,base,donor,strength:.5,eyes_mask:mask.glasses_mask,protected:mask.protected,confirmation:ref(18),source_face_id:id(23),donor_face_id:id(24),source_faces:source.source_faces,donor_faces:candidate.donor_faces},target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel};
afterEach(()=>{cleanup();memory.clear();});
it('selects exact producer refs and submits the server-prepared open eyes intent',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const prepareOpenEyes=vi.fn(async(_project:string,_body:unknown,_key:string)=>prepared);
 const submitOpenEyes=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={
  me:vi.fn(async()=>({id:id(30)})),
  v2Binding:vi.fn(async(asset:string)=>asset===id(31)?base:donor),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  shootSessions:vi.fn(async()=>({items:[session],next_cursor:null})),
  openEyesSourceCandidates:vi.fn(async()=>({items:[source],next_cursor:null})),
  openEyesMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  openEyesDonorCandidates:vi.fn(async()=>({source_faces:source.source_faces,items:[candidate],next_cursor:null})),
  prepareOpenEyes,submitOpenEyes,
  get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
 } as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorOpenEyes api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} collection={session.collection_id} onSubmitted={onSubmitted}/>);
 async function choose(name:string){await screen.findByText(name);const radio=screen.getByRole('radio',{name});await waitFor(()=>expectEnabled(radio));await clickWhenEnabled(radio);}
 await choose('Shoot 1: Portrait shoot');await choose('Source face 1');await choose('Donor frame 1');await choose('Mask pair 1');
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Review open eyes request'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Review open eyes request'}));
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 const selection=prepareOpenEyes.mock.calls[0][1] as Record<string,unknown>;
 expect(selection).toMatchObject({source_observation_id:source.observation_id,donor_observation_id:candidate.observation_id,
  eyes_mask:mask.glasses_mask,protected:mask.protected,target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel});
 expect(prepareOpenEyes.mock.calls[0][2]).toBe(submitOpenEyes.mock.calls[0][2]);
});

it('rejects a donor catalog response bound to different source-face evidence',async()=>{
 const prepareOpenEyes=vi.fn();
 const api={
  me:vi.fn(async()=>({id:id(30)})),
  v2Binding:vi.fn(async()=>base),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  shootSessions:vi.fn(async()=>({items:[session],next_cursor:null})),
  openEyesSourceCandidates:vi.fn(async()=>({items:[source],next_cursor:null})),
  openEyesMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  openEyesDonorCandidates:vi.fn(async()=>({source_faces:ref(29),items:[candidate],next_cursor:null})),
  prepareOpenEyes,
 } as unknown as OwnerAPI;
 render(<EditorOpenEyes api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} collection={session.collection_id} onSubmitted={vi.fn()}/>);
 const shoot=await screen.findByRole('radio',{name:'Shoot 1: Portrait shoot'});
 await waitFor(()=>expectEnabled(shoot));
 await clickWhenEnabled(shoot);
 const face=await screen.findByRole('radio',{name:'Source face 1'});
 await waitFor(()=>expectEnabled(face));
 await clickWhenEnabled(face);
 await screen.findByRole('alert');
 expect(screen.getByRole('alert').textContent).toContain('Donor observations are unavailable.');
 expect(screen.queryByRole('radio',{name:'Donor frame 1'})).toBeNull();
 expect(prepareOpenEyes).not.toHaveBeenCalled();
});

it('blocks saved open-eyes recovery when live source bytes change within the same version',async()=>{
 const origin='https://photo.test',user=id(30),tenant=id(32),project=id(6);
 const journal=new OpenEyesAttemptStore(AsyncStorage,origin,user,tenant,project,base.asset_version_id);
 await journal.write({schema_version:1,project,key:id(40),body:prepared,ticket:null});
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>({...base,source:{...base.source,sha256:'d'.repeat(64)}})),
  prepareOpenEyes:vi.fn(),submitOpenEyes:vi.fn(),createRenderInputSnapshot:vi.fn(),shootSessions:vi.fn()} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();
 render(<EditorOpenEyes api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} onSubmitted={onSubmitted}/>);
 await screen.findByRole('alert');
 expect(screen.queryByRole('button',{name:'Prepare and submit'})).toBeNull();
 expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();expect(api.submitOpenEyes).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
 expect((await journal.read())?.body).toEqual(prepared);
});

it('blocks open-eyes POST when capability is withdrawn during preparation without rerender',async()=>{
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 let available=true;let finish!:(value:typeof prepared)=>void;
 const prepareOpenEyes=vi.fn((_project:string,_body:unknown,_key:string)=>new Promise<typeof prepared>(resolve=>{finish=resolve;}));
 const submitOpenEyes=vi.fn(async(_project:string,_body:unknown,_key:string)=>ticket);
 const api={
  me:vi.fn(async()=>({id:id(30)})),
  v2Binding:vi.fn(async(asset:string)=>asset===id(31)?base:donor),
  createRenderInputSnapshot:vi.fn(async()=>({id:id(17)})),
  shootSessions:vi.fn(async()=>({items:[session],next_cursor:null})),
  openEyesSourceCandidates:vi.fn(async()=>({items:[source],next_cursor:null})),
  openEyesMaskPairs:vi.fn(async()=>({items:[mask],next_cursor:null})),
  openEyesDonorCandidates:vi.fn(async()=>({source_faces:source.source_faces,items:[candidate],next_cursor:null})),
  prepareOpenEyes,submitOpenEyes,
  get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
 } as unknown as OwnerAPI;
 const onSubmitted=vi.fn();render(<EditorOpenEyes operationAvailable={()=>available} api={api} origin="https://photo.test" tenant={id(32)} project={id(6)} asset={id(31)} version={base.asset_version_id} collection={session.collection_id} onSubmitted={onSubmitted}/>);
 async function choose(name:string){await screen.findByText(name);const radio=screen.getByRole('radio',{name});await waitFor(()=>expectEnabled(radio));await clickWhenEnabled(radio);}
 await choose('Shoot 1: Portrait shoot');await choose('Source face 1');await choose('Donor frame 1');await choose('Mask pair 1');
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Review open eyes request'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Review open eyes request'}));
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));
 await clickWhenEnabled(screen.getByRole('button',{name:'Prepare and submit'}));
 await waitFor(()=>expect(prepareOpenEyes).toHaveBeenCalledTimes(1));
 available=false;await act(async()=>finish(prepared));
 expect(submitOpenEyes).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});

it('opens accepted open-eyes after worker capability disappears without submitting',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30);
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const storageKey='photo.open-eyes-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 memory.set(storageKey,JSON.stringify({schema_version:1,project,key:'accepted:recovery',body:prepared,ticket}));
 const api={me:vi.fn(async()=>({id:user})),v2Binding:vi.fn(async()=>base),get:vi.fn(async()=>({run_id:ticket.run_id,workflow_id:ticket.workflow_id})),
  createRenderInputSnapshot:vi.fn(),shootSessions:vi.fn(),prepareOpenEyes:vi.fn(),submitOpenEyes:vi.fn()} as unknown as OwnerAPI;
 const onSubmitted=vi.fn();
 render(<EditorOpenEyes api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} operationAvailable={()=>false} onSubmitted={onSubmitted}/>);
 const open=await screen.findByRole('button',{name:'Open result'});
 await waitFor(()=>expectEnabled(open));await clickWhenEnabled(open);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(api.submitOpenEyes).not.toHaveBeenCalled();expect(api.prepareOpenEyes).not.toHaveBeenCalled();expect(api.createRenderInputSnapshot).not.toHaveBeenCalled();
});

it('replays a lost open-eyes POST with exact durable identity after worker withdrawal',async()=>{
 const origin='https://photo.test',tenant=id(32),project=id(6),user=id(30),key='lost:eyes';
 const storageKey='photo.open-eyes-attempt.v1:'+JSON.stringify([origin,user,tenant,project,base.asset_version_id]);
 memory.set(storageKey,JSON.stringify({schema_version:1,project,key,body:prepared,ticket:null}));
 const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
 const submitOpenEyes=vi.fn().mockResolvedValue(ticket),prepareOpenEyes=vi.fn(),onSubmitted=vi.fn();
 const api={me:async()=>({id:user}),v2Binding:async()=>base,get:async()=>ticket,submitOpenEyes,prepareOpenEyes} as unknown as OwnerAPI;
 render(<EditorOpenEyes api={api} origin={origin} tenant={tenant} project={project} asset={id(31)} version={base.asset_version_id} operationAvailable={()=>false} onSubmitted={onSubmitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry submission'});await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(submitOpenEyes).toHaveBeenCalledWith(project,prepared,key,expect.any(AbortSignal));expect(prepareOpenEyes).not.toHaveBeenCalled();
});
