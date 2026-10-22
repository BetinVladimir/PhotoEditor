// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,render,waitFor} from '@testing-library/react';
import type {OwnerAPI} from './owner-api';
const state=vi.hoisted(()=>({handlers:[] as Array<()=>void>,values:new Map<string,string>()}));
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
vi.mock('react-native',async()=>{const native=await import('react-native-web'),React=await import('react');return {...native,Pressable:(props:any)=>{state.handlers.push(props.onPress);return React.createElement(native.Pressable,props);}};});
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>state.values.get(k)??null,setItem:async(k:string,v:string)=>{state.values.set(k,v);},removeItem:async(k:string)=>{state.values.delete(k);}}}));
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

afterEach(()=>{cleanup();state.handlers=[];state.values.clear();});
it.each(['unmount','scope'] as const)('refuses a retained durable retry after %s before actor or inference IO',async(kind)=>{
 const props={origin:'https://photo.test',user:id(30),tenant:id(32),project:id(6),baseVersion:base.asset_version_id};
 const key='photo.open-eyes-attempt.v1:'+JSON.stringify([props.origin,props.user,props.tenant,props.project,props.baseVersion]);
 state.values.set(key,JSON.stringify({schema_version:1,project:props.project,key:'scope:retry',body:prepared,ticket:null}));
 const me=vi.fn(),submitOpenEyes=vi.fn(),api={me,submitOpenEyes} as unknown as OwnerAPI;
 const view=render(<OpenEyesControls {...props} api={api} operationAvailable={()=>false} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(state.handlers.length).toBeGreaterThan(1));const old=state.handlers.at(-1)!;
 if(kind==='unmount')view.unmount();else view.rerender(<OpenEyesControls {...props} project={id(99)} api={api} operationAvailable={()=>false} onSubmitted={vi.fn()}/>);
 await act(async()=>{old();});expect(me).not.toHaveBeenCalled();expect(submitOpenEyes).not.toHaveBeenCalled();
});
it('retains NEW denial without actor, preparation or inference IO',async()=>{
 const me=vi.fn(),prepareOpenEyes=vi.fn(),submitOpenEyes=vi.fn();
 const props={origin:'https://photo.test',user:id(30),tenant:id(32),project:id(6),baseVersion:base.asset_version_id};
 render(<OpenEyesControls {...props} api={{me,prepareOpenEyes,submitOpenEyes} as unknown as OwnerAPI} operationAvailable={()=>false} onSubmitted={vi.fn()}/>);
 await waitFor(()=>expect(state.handlers.length).toBeGreaterThan(1));await act(async()=>{state.handlers.at(-1)!();});
 expect(me).not.toHaveBeenCalled();expect(prepareOpenEyes).not.toHaveBeenCalled();expect(submitOpenEyes).not.toHaveBeenCalled();
});
