// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
const memory=new Map<string,string>();
vi.mock('@react-native-async-storage/async-storage',()=>({default:{getItem:async(k:string)=>memory.get(k)??null,setItem:async(k:string,v:string)=>{memory.set(k,v);},removeItem:async(k:string)=>{memory.delete(k);}}}));
vi.mock('./glare-source-controls',()=>({GlareSourceControls:()=> <div>Recovered source request</div>}));
import {EditorGlareSource} from './editor-glare-source';
import type {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const ref=(n:number)=>({artifact_id:id(n),sha256:'a'.repeat(64)});
const base={asset_version_id:id(5),source:ref(6),canvas_hash:'b'.repeat(64)};
const props={origin:'https://source-picker.test',tenant:id(31),project:id(32),asset:id(33),version:id(5),onSubmitted:vi.fn()};
afterEach(()=>{cleanup();memory.clear();});
it('restores immutable recovery before capability, new snapshot, shoot or donor discovery',async()=>{
 const body={schema_version:1,shoot_session_id:id(1),snapshot_id:id(2),ordinal:0,source_observation_id:id(3),params_proposal:{schema_version:1,mode:'generative',donor:null,donor_faces:null,base,strength:.5,glasses_mask:ref(9),protected:ref(10),confirmation:ref(11),source_faces:ref(12),model_lock_sha:'d'.repeat(64),calibration:ref(14)}};
 memory.set('photo.glare-source-attempt.v1:'+JSON.stringify([props.origin,id(30),props.tenant,props.project,props.version]),JSON.stringify({schema_version:1,project:props.project,key:'source:restore',body,ticket:null}));
 const glareSourceAvailable=vi.fn(),createRenderInputSnapshot=vi.fn(),shootSessions=vi.fn(),glareDonorCandidates=vi.fn();
 render(<EditorGlareSource {...props} api={{me:async()=>({id:id(30)}),v2Binding:async()=>base,glareSourceAvailable,createRenderInputSnapshot,shootSessions,glareDonorCandidates} as unknown as OwnerAPI}/>);
 await screen.findByText('Recovered source request');expect(glareSourceAvailable).not.toHaveBeenCalled();expect(createRenderInputSnapshot).not.toHaveBeenCalled();expect(shootSessions).not.toHaveBeenCalled();expect(glareDonorCandidates).not.toHaveBeenCalled();
});
it('does not create a snapshot from an old capability response after account/API scope changes',async()=>{
 let resolve!:(v:boolean)=>void;const oldAvailable=vi.fn(()=>new Promise<boolean>(r=>{resolve=r;})),createRenderInputSnapshot=vi.fn(),shootSessions=vi.fn();
 const oldAPI={me:async()=>({id:id(30)}),v2Binding:async()=>base,glareSourceAvailable:oldAvailable,createRenderInputSnapshot,shootSessions} as unknown as OwnerAPI;
 const view=render(<EditorGlareSource {...props} api={oldAPI}/>);await waitFor(()=>expect(oldAvailable).toHaveBeenCalledOnce());
 const currentAvailable=vi.fn().mockResolvedValue(false);view.rerender(<EditorGlareSource {...props} api={{me:async()=>({id:id(40)}),v2Binding:async()=>base,glareSourceAvailable:currentAvailable} as unknown as OwnerAPI}/>);
 await waitFor(()=>expect(currentAvailable).toHaveBeenCalledOnce());resolve(true);await screen.findByText('Generative glare choices are unavailable.');expect(createRenderInputSnapshot).not.toHaveBeenCalled();expect(shootSessions).not.toHaveBeenCalled();
});
