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
import {EditorOpenEyes} from './editor-open-eyes';
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
 params_proposal:{schema_version:1 as const,base,donor,strength:.5,eyes_mask:mask.glasses_mask,protected:mask.protected,confirmation:ref(18),source_faces:source.source_faces,donor_faces:candidate.donor_faces},target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel};
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
 async function choose(name:string){await screen.findByText(name);const radio=screen.getByRole('radio',{name});await waitFor(()=>expect(radio.getAttribute('aria-disabled')).not.toBe('true'));fireEvent.click(radio);}
 await choose('Shoot 1: Portrait shoot');await choose('Source face 1');await choose('Donor frame 1');await choose('Mask pair 1');
 await waitFor(()=>expect(screen.getByRole('button',{name:'Review open eyes request'}).getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByRole('button',{name:'Review open eyes request'}));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(screen.getByRole('button',{name:'Prepare and submit'}));
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
 await waitFor(()=>expect(shoot.getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(shoot);
 const face=await screen.findByRole('radio',{name:'Source face 1'});
 await waitFor(()=>expect(face.getAttribute('aria-disabled')).not.toBe('true'));
 fireEvent.click(face);
 await screen.findByRole('alert');
 expect(screen.getByRole('alert').textContent).toContain('Donor observations are unavailable.');
 expect(screen.queryByRole('radio',{name:'Donor frame 1'})).toBeNull();
 expect(prepareOpenEyes).not.toHaveBeenCalled();
});
