// @vitest-environment jsdom
import React from 'react';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import type {OwnerAPI} from './owner-api';
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
vi.mock('./local-portrait-attempt-store',()=>({LocalPortraitAttemptStore:class{async read(){return null;}}}));
vi.mock('./reflection-mask-pairs',()=>({collectReflectionMaskPairs:async()=>[{mask:{artifact_id:'mask'},protected:{artifact_id:'protected'},target_pixel:{artifact_id:'target'},protected_pixel:{artifact_id:'protected-pixel'}}]}));
vi.mock('./local-portrait-intent',()=>({freezeLocalPortraitSelection:(value:unknown)=>value,verifyLocalPortraitRecoverySource:()=>{}}));
vi.mock('./local-portrait-controls',()=>({LocalPortraitControls:({disabled,operationAvailable}:{disabled:boolean;operationAvailable:()=>boolean})=><button disabled={disabled||!operationAvailable()}>Submit selected portrait</button>}));
import {EditorLocalPortrait} from './editor-local-portrait';
afterEach(cleanup);
it('disables a confirmed hair request when only clothing remains available',async()=>{
 const api={me:vi.fn().mockResolvedValue({id:'user'}),v2Binding:vi.fn().mockResolvedValue({asset_version_id:'version'}),createRenderInputSnapshot:vi.fn().mockResolvedValue({id:'snapshot'})} as unknown as OwnerAPI;
 const props={api,origin:'http://localhost',tenant:'tenant',project:'project',asset:'asset',version:'version',hairAvailable:true,clothingAvailable:true,onSubmitted:vi.fn()};
 const view=render(<EditorLocalPortrait {...props}/>);
 fireEvent.click(await screen.findByText('Mask pair 1'));
 fireEvent.click(screen.getByRole('button',{name:'Review local portrait request'}));
 expect((screen.getByText('Submit selected portrait') as HTMLButtonElement).disabled).toBe(false);
 view.rerender(<EditorLocalPortrait {...props} hairAvailable={false}/>);
 expect((screen.getByText('Submit selected portrait') as HTMLButtonElement).disabled).toBe(true);
 view.rerender(<EditorLocalPortrait {...props}/>);
 expect((screen.getByText('Submit selected portrait') as HTMLButtonElement).disabled).toBe(false);
});
