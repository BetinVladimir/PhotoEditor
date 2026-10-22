// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
const memory=new Map<string,string>();
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>memory.get(k)??null,setItem:async(k:string,v:string)=>{memory.set(k,v);},removeItem:async(k:string)=>{memory.delete(k);}}}));
import {GlareSourceControls} from './glare-source-controls';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {GlareSourcePreparation,GlareSourceIntent} from './glare-source-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const selection:GlareSourcePreparation={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,base:{asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)},strength:.5,glasses_mask:ref(9),protected:ref(10),target_pixel:ref(15),protected_pixel:ref(16),source_faces:ref(12),source_observation_id:id(3)};
const prepared:GlareSourceIntent={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,source_observation_id:id(3),params_proposal:{schema_version:1,mode:'generative',donor:null,donor_faces:null,base:selection.base,strength:.5,glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),model_lock_sha:'d'.repeat(64),calibration:ref(14)}};
const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
const props={origin:'https://source-glare.test',user:id(30),tenant:id(31),project:id(32),baseVersion:id(5),selection};
afterEach(()=>{cleanup();memory.clear();});
it.each(['before','after'] as const)('withdraws source editing when the actor changes %s submission',async(stage)=>{
 const me=vi.fn().mockResolvedValueOnce({id:stage==='before'?id(99):props.user}).mockResolvedValue({id:id(99)});
 const prepareGlareSource=vi.fn().mockResolvedValue(prepared),submitGlareSource=vi.fn().mockResolvedValue(ticket),onSubmitted=vi.fn(),onAccessDenied=vi.fn();
 render(<GlareSourceControls {...props} api={{me,glareSourceAvailable:async()=>true,prepareGlareSource,submitGlareSource,get:async()=>ticket} as unknown as OwnerAPI} onSubmitted={onSubmitted} onAccessDenied={onAccessDenied}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));await clickWhenEnabled(screen.getByText('Prepare and submit'));
 await screen.findByText(/Access is no longer available/);expect(onAccessDenied).toHaveBeenCalledOnce();expect(onSubmitted).not.toHaveBeenCalled();
 expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).toBe('true');
 if(stage==='before')expect(prepareGlareSource).not.toHaveBeenCalled();else expect([...memory.values()].some(v=>JSON.parse(v).ticket?.task_id===ticket.task_id)).toBe(true);
});
it.each([['prepare',401],['prepare',403],['submit',401],['submit',403],['workflow',401],['workflow',403]] as const)('withdraws source authority after %s %s denial and retains durable recovery',async(stage,status)=>{
 const glareSourceAvailable=vi.fn().mockResolvedValue(true),prepareGlareSource=vi.fn().mockResolvedValue(prepared),submitGlareSource=vi.fn().mockResolvedValue(ticket),get=vi.fn().mockResolvedValue(ticket);
 const denied=new APIError(status,'FORBIDDEN');if(stage==='prepare')prepareGlareSource.mockRejectedValue(denied);else if(stage==='submit')submitGlareSource.mockRejectedValue(denied);else get.mockRejectedValue(denied);
 const onSubmitted=vi.fn();render(<GlareSourceControls {...props} api={{me:async()=>({id:props.user}),glareSourceAvailable,prepareGlareSource,submitGlareSource,get} as unknown as OwnerAPI} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));await clickWhenEnabled(screen.getByText('Prepare and submit'));
 await screen.findByText(/Access is no longer available/);expect(onSubmitted).not.toHaveBeenCalled();expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).toBe('true');
 const saved=[...memory.values()].map(v=>JSON.parse(v)).filter(v=>v.schema_version===1);
 if(stage==='prepare'){expect(saved).toHaveLength(0);expect(submitGlareSource).not.toHaveBeenCalled();}else{expect(saved).toHaveLength(1);expect(saved[0].body).toEqual(prepared);expect(saved[0].ticket).toEqual(stage==='workflow'?ticket:null);}
});
it('opens an accepted immutable journal without capability, preparation or repeated POST',async()=>{
 const key='photo.glare-source-attempt.v1:'+JSON.stringify([props.origin,props.user,props.tenant,props.project,props.baseVersion]);
 memory.set(key,JSON.stringify({schema_version:1,project:props.project,key:'source:accepted',body:prepared,ticket}));
 const glareSourceAvailable=vi.fn(),prepareGlareSource=vi.fn(),submitGlareSource=vi.fn(),get=vi.fn().mockResolvedValue(ticket),onSubmitted=vi.fn();
 render(<GlareSourceControls {...props} selection={undefined} api={{me:async()=>({id:props.user}),glareSourceAvailable,prepareGlareSource,submitGlareSource,get} as unknown as OwnerAPI} onSubmitted={onSubmitted}/>);
 await screen.findByText('Open result');await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Open result'})));await clickWhenEnabled(screen.getByText('Open result'));
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(get).toHaveBeenCalledOnce();expect(glareSourceAvailable).not.toHaveBeenCalled();expect(prepareGlareSource).not.toHaveBeenCalled();expect(submitGlareSource).not.toHaveBeenCalled();
});
it('does not prepare or submit when the qualified installation is unavailable',async()=>{
 const prepareGlareSource=vi.fn(),submitGlareSource=vi.fn();render(<GlareSourceControls {...props} api={{me:async()=>({id:props.user}),glareSourceAvailable:async()=>false,prepareGlareSource,submitGlareSource} as unknown as OwnerAPI} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));await clickWhenEnabled(screen.getByText('Prepare and submit'));await screen.findByText(/current qualified installation/);expect(prepareGlareSource).not.toHaveBeenCalled();expect(submitGlareSource).not.toHaveBeenCalled();
});
it('persists a late accepted POST after unmount without reading workflow or navigating',async()=>{
 let resolve!:(v:typeof ticket)=>void;const submitGlareSource=vi.fn(()=>new Promise<typeof ticket>(r=>{resolve=r;})),get=vi.fn(),onSubmitted=vi.fn();
 const view=render(<GlareSourceControls {...props} api={{me:async()=>({id:props.user}),glareSourceAvailable:async()=>true,prepareGlareSource:async()=>prepared,submitGlareSource,get} as unknown as OwnerAPI} onSubmitted={onSubmitted}/>);
 await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));await clickWhenEnabled(screen.getByText('Prepare and submit'));await waitFor(()=>expect(submitGlareSource).toHaveBeenCalledOnce());view.unmount();resolve(ticket);
 await waitFor(()=>expect([...memory.values()].some(v=>JSON.parse(v).ticket?.task_id===ticket.task_id)).toBe(true));expect(get).not.toHaveBeenCalled();expect(onSubmitted).not.toHaveBeenCalled();
});

it('replays unknown immutable source glare without live capability discovery but never prepares a new command',async()=>{
 const key='photo.glare-source-attempt.v1:'+JSON.stringify([props.origin,props.user,props.tenant,props.project,props.baseVersion]);
 memory.set(key,JSON.stringify({schema_version:1,project:props.project,key:'source:lost',body:prepared,ticket:null}));
 const glareSourceAvailable=vi.fn(),prepareGlareSource=vi.fn(),submitGlareSource=vi.fn().mockResolvedValue(ticket),onSubmitted=vi.fn();
 const api={me:async()=>({id:props.user}),glareSourceAvailable,prepareGlareSource,submitGlareSource,get:async()=>ticket} as unknown as OwnerAPI;
 render(<GlareSourceControls {...props} api={api} operationAvailable={()=>false} onSubmitted={onSubmitted}/>);
 const retry=await screen.findByRole('button',{name:'Retry submission'});await waitFor(()=>expectEnabled(retry));await clickWhenEnabled(retry);
 await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));expect(glareSourceAvailable).not.toHaveBeenCalled();expect(prepareGlareSource).not.toHaveBeenCalled();
 expect(submitGlareSource).toHaveBeenCalledWith(props.project,prepared,'source:lost',expect.any(AbortSignal));
});
