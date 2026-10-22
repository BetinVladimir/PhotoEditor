import {clickWhenEnabled} from './test/ui-readiness';
// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {MultiPreviewView,multiPreviewDisplayScope} from './multi-preview-view';
import {LocaleProvider} from './locale-context';
import type {MultiProposal} from './multi-proposal';
import type {MultiIntent} from './multi-intent';
// UI transport failure fixtures only; do not establish contract/codec validity.
const proposal={tenant_id:'tenant',project_id:'project',operation:'optical.panorama',producer_task_id:'producer',result_id:'result',result:{sha256:'a'.repeat(64)}} as MultiProposal;
const intent={} as MultiIntent;
afterEach(()=>{cleanup();vi.restoreAllMocks();});
it('invalidates display scope when a source changes under the same result',()=>{
 const changed={...proposal,sources:[{source:{artifact_id:'new-source',sha256:'b'.repeat(64)}}]} as MultiProposal;
 expect(multiPreviewDisplayScope(changed,intent)).not.toBe(multiPreviewDisplayScope(proposal,intent));
});
it('retries an unavailable preview and forwards variant',async()=>{
 const multiPreview=vi.fn().mockRejectedValue(new Error('not ready'));const api={multiPreview};
 render(<LocaleProvider><MultiPreviewView api={api} proposal={proposal} intent={intent}/></LocaleProvider>);
 await screen.findByText('Preview unavailable. Try again later.');
 await clickWhenEnabled(screen.getByRole('button',{name:'Reload preview'}));await waitFor(()=>expect(multiPreview).toHaveBeenCalledTimes(2));
 await clickWhenEnabled(screen.getByRole('button',{name:'Show 2560px preview'}));await waitFor(()=>expect(multiPreview.mock.calls[2][2]).toBe('preview2560'));
});
it('aborts pending request when unmounted',()=>{
 const multiPreview=vi.fn((_proposal:MultiProposal,_intent:MultiIntent,_variant?:string,_signal?:AbortSignal)=>new Promise<never>(()=>{}));
 const view=render(<LocaleProvider><MultiPreviewView api={{multiPreview}} proposal={proposal} intent={intent}/></LocaleProvider>);
 const signal=multiPreview.mock.calls[0]?.[3] as AbortSignal;
 expect(signal.aborted).toBe(false);view.unmount();expect(signal.aborted).toBe(true);
});
