// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {PerspectiveQualityView,perspectiveQualityDisplayScope} from './perspective-quality-view';
import {LocaleProvider} from './locale-context';
import type {PerspectiveProposal} from './perspective-proposal';
import type {OwnerAPI} from './owner-api';
import {APIError} from '@photoeditor/api-client';

// Transport-only fixture; quality payload validity is covered by the parser tests.
const proposal={tenant_id:'tenant',project_id:'project',producer_task_id:'producer',result_id:'result',
 result:{sha256:'a'.repeat(64)},changed_mask:{sha256:'b'.repeat(64)},canvas:{width:100,height:80}} as PerspectiveProposal;
afterEach(()=>{cleanup();vi.restoreAllMocks();});
it.each([401,403,404,410])('withdraws parent quality authority on definitive denial %s',async(status)=>{
 const onAccessDenied=vi.fn(),perspectiveQuality=vi.fn().mockRejectedValue(new APIError(status,'FORBIDDEN'));
 render(<LocaleProvider><PerspectiveQualityView api={{perspectiveQuality} as unknown as OwnerAPI} proposal={proposal} origin="https://app.example" onAccessDenied={onAccessDenied}/></LocaleProvider>);
 await screen.findByText('Access is no longer available.');await waitFor(()=>expect(onAccessDenied).toHaveBeenCalledOnce());expect(perspectiveQuality).toHaveBeenCalledOnce();expect(screen.queryByRole('button')).toBeNull();
});

it('invalidates quality display when geometry or mask changes under the same result',()=>{
 expect(perspectiveQualityDisplayScope({...proposal,canvas:{...proposal.canvas,width:101}})).not.toBe(perspectiveQualityDisplayScope(proposal));
 expect(perspectiveQualityDisplayScope({...proposal,changed_mask:{...proposal.changed_mask,sha256:'c'.repeat(64)}})).not.toBe(perspectiveQualityDisplayScope(proposal));
});

it('aborts old quality read and ignores its late report after geometry changes',async()=>{
 let resolveOld!:(value:unknown)=>void;
 const perspectiveQuality=vi.fn((_proposal:PerspectiveProposal,_signal?:AbortSignal)=>new Promise<unknown>(resolve=>{resolveOld=resolve;}));
 const api={perspectiveQuality} as unknown as OwnerAPI;
 const view=render(<LocaleProvider><PerspectiveQualityView api={api} proposal={proposal} origin="https://app.example"/></LocaleProvider>);
 const oldFinish=resolveOld,oldSignal=perspectiveQuality.mock.calls[0]?.[1] as AbortSignal;
 view.rerender(<LocaleProvider><PerspectiveQualityView api={api} proposal={{...proposal,canvas:{...proposal.canvas,width:101}}} origin="https://app.example"/></LocaleProvider>);
 expect(oldSignal.aborted).toBe(true);
 await act(async()=>{oldFinish({quality:{status:'pass',reasons:[],technical:{clipped_black_fraction:0,clipped_white_fraction:0}}});});
 expect(screen.queryByText('Technical checks passed')).toBeNull();
 view.unmount();
});
