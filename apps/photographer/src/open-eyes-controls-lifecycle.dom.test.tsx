// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
const state=vi.hoisted(()=>({handlers:[] as Array<{name:string;invoke:()=>void}>,values:new Map<string,string>(),read:null as null|(()=>Promise<string|null>)}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.read?state.read():state.values.get(k)??null,setItem:async(k:string,v:string)=>{state.values.set(k,v);},removeItem:async(k:string)=>{state.values.delete(k);}}}));
import {OpenEyesControls} from './open-eyes-controls';
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

const props={origin:'https://photo.test',user:id(30),tenant:id(32),project:id(6),baseVersion:base.asset_version_id};
const key='photo.open-eyes-attempt.v1:'+JSON.stringify([props.origin,props.user,props.tenant,props.project,props.baseVersion]);
const receipt={run_id:id(20),workflow_id:id(21),task_id:id(22),state:'queued' as const};
afterEach(()=>{cleanup();state.handlers=[];state.values.clear();state.read=null;vi.useRealTimers();});
it('bounds a stalled journal bootstrap and exposes readonly retry without new inference',async()=>{
 vi.useFakeTimers();let finish!:(value:null)=>void;state.read=()=>new Promise(resolve=>{finish=resolve;});const submitOpenEyes=vi.fn();
 render(<OpenEyesControls {...props} api={{submitOpenEyes} as unknown as OwnerAPI} onSubmitted={vi.fn()}/>);
 await act(async()=>{await vi.advanceTimersByTimeAsync(8000);});expect(screen.getByRole('button',{name:'Retry loading'})).toBeTruthy();expect(submitOpenEyes).not.toHaveBeenCalled();
 state.read=null;await act(async()=>{finish(null);await Promise.resolve();});
});
it('Stop preserves an unknown request and a retained old Stop cannot cancel the replacement retry',async()=>{
 state.values.set(key,JSON.stringify({schema_version:1,project:props.project,key:'lifecycle:exact',body:prepared,ticket:null}));
 let finish!:(value:typeof receipt)=>void;const submitOpenEyes=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{finish=resolve;})).mockResolvedValue(receipt),onSubmitted=vi.fn();
 const api={me:async()=>({id:props.user}),submitOpenEyes,get:async()=>receipt} as unknown as OwnerAPI;
 render(<OpenEyesControls {...props} api={api} operationAvailable={()=>false} onSubmitted={onSubmitted}/>);
 await screen.findByRole('button',{name:'Retry submission'});fireEvent.click(screen.getByRole('button',{name:'Retry submission'}));
 await waitFor(()=>expect(submitOpenEyes).toHaveBeenCalledTimes(1));const oldStop=state.handlers.filter(h=>h.name==='Stop loading').at(-1)!.invoke;
 await act(async()=>{oldStop();});expect(onSubmitted).not.toHaveBeenCalled();expect(JSON.parse(state.values.get(key)!).key).toBe('lifecycle:exact');
 fireEvent.click(screen.getByRole('button',{name:'Retry submission'}));await act(async()=>{oldStop();});await waitFor(()=>expect(onSubmitted).toHaveBeenCalledWith(receipt));
 expect(submitOpenEyes.mock.calls[1].slice(0,3)).toEqual(submitOpenEyes.mock.calls[0].slice(0,3));
 await act(async()=>{finish({...receipt,run_id:id(99)});});expect(onSubmitted).toHaveBeenCalledTimes(1);
});

vi.mock('react-native-paper',async importOriginal=>{const paper=await importOriginal<typeof import('react-native-paper')>(),React=await import('react');const observe=(props:any)=>{const unwrap=(value:any):any=>typeof value==='string'?value:value?.props?unwrap(value.props.children):value;const label=unwrap(props.children);state.handlers.push({name:label,invoke:props.onPress});};return {...paper,Button:(props:any)=>{observe(props);return React.createElement(paper.Button,props)},TouchableRipple:(props:any)=>{observe(props);return React.createElement(paper.TouchableRipple,props)}};});
