// @vitest-environment jsdom
import {expectEnabled,clickWhenEnabled} from './test/ui-readiness';
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
const memory=vi.hoisted(()=>new Map<string,string>());
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>memory.get(k)??null,setItem:async(k:string,v:string)=>{memory.set(k,v);},removeItem:async(k:string)=>{memory.delete(k);}}}));
import {OpenEyesControls} from './open-eyes-controls';
import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {OpenEyesSelection,OpenEyesIntent} from './open-eyes-intent';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(1),source:ref(2),canvas_hash:'b'.repeat(64)},donor={asset_version_id:id(3),source:ref(4),canvas_hash:'c'.repeat(64)};
const selection:OpenEyesSelection={schema_version:1,shoot_session_id:id(5),snapshot_id:id(17),ordinal:0,base,donor,strength:.5,eyes_mask:ref(13),protected:ref(14),target_pixel:ref(15),protected_pixel:ref(16),source_faces:ref(9),donor_faces:ref(12),source_observation_id:id(8),donor_observation_id:id(10)};
const prepared:OpenEyesIntent={schema_version:1,shoot_session_id:id(5),snapshot_id:id(17),ordinal:0,source_observation_id:id(8),donor_observation_id:id(10),target_pixel:ref(15),protected_pixel:ref(16),params_proposal:{schema_version:1,base,donor,strength:.5,eyes_mask:ref(13),protected:ref(14),confirmation:ref(18),source_face_id:id(23),donor_face_id:id(24),source_faces:ref(9),donor_faces:ref(12)}};
const ticket={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
const props={origin:'https://eyes.test',user:id(30),tenant:id(32),project:id(6),baseVersion:base.asset_version_id,selection};
afterEach(()=>{cleanup();memory.clear();});
async function submit(){await waitFor(()=>expectEnabled(screen.getByRole('button',{name:'Prepare and submit'})));await clickWhenEnabled(screen.getByText('Prepare and submit'));}
it.each([401,403,404,410])('withdraws denied submission %s while preserving the immutable journal',async(status)=>{
 const onAccessDenied=vi.fn(),onSubmitted=vi.fn(),post=vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'));
 render(<OpenEyesControls {...props} api={{me:async()=>({id:props.user}),prepareOpenEyes:async()=>prepared,submitOpenEyes:post} as unknown as OwnerAPI} onSubmitted={onSubmitted} onAccessDenied={onAccessDenied}/>);
 await submit();await screen.findByText(/Access is no longer available/);expect(onAccessDenied).toHaveBeenCalledOnce();expect(onSubmitted).not.toHaveBeenCalled();expect(screen.getByRole('button',{name:'Prepare and submit'}).getAttribute('aria-disabled')).toBe('true');
 expect([...memory.values()].some(v=>JSON.parse(v).body?.params_proposal.confirmation.artifact_id===id(18))).toBe(true);
});
it.each(['before','after'] as const)('refuses navigation when the authenticated actor changes %s submission',async(stage)=>{
 const me=vi.fn().mockResolvedValueOnce({id:stage==='before'?id(99):props.user}).mockResolvedValue({id:id(99)}),prepareOpenEyes=vi.fn().mockResolvedValue(prepared),post=vi.fn().mockResolvedValue(ticket),onAccessDenied=vi.fn(),onSubmitted=vi.fn();
 render(<OpenEyesControls {...props} api={{me,prepareOpenEyes,submitOpenEyes:post,get:async()=>ticket} as unknown as OwnerAPI} onSubmitted={onSubmitted} onAccessDenied={onAccessDenied}/>);
 await submit();await screen.findByText(/Access is no longer available/);expect(onAccessDenied).toHaveBeenCalledOnce();expect(onSubmitted).not.toHaveBeenCalled();
 if(stage==='before')expect(prepareOpenEyes).not.toHaveBeenCalled();else expect([...memory.values()].some(v=>JSON.parse(v).ticket?.task_id===ticket.task_id)).toBe(true);
});
it('retries an ambiguous response with the same body and key',async()=>{
 const prepareOpenEyes=vi.fn().mockResolvedValue(prepared),post=vi.fn().mockRejectedValueOnce(new Error('network')).mockResolvedValue(ticket),onSubmitted=vi.fn();
 render(<OpenEyesControls {...props} api={{me:async()=>({id:props.user}),prepareOpenEyes,submitOpenEyes:post,get:async()=>ticket} as unknown as OwnerAPI} onSubmitted={onSubmitted}/>);
 await submit();await screen.findByText(/Submission is uncertain/);fireEvent.click(screen.getByText('Retry submission'));await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(ticket));
 expect(prepareOpenEyes).toHaveBeenCalledOnce();expect(post.mock.calls[0].slice(0,3)).toEqual(post.mock.calls[1].slice(0,3));
});
