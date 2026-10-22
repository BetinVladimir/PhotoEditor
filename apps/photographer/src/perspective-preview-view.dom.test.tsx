import {clickWhenEnabled} from './test/ui-readiness';
// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {PerspectivePreviewView,perspectivePreviewDisplayScope} from './perspective-preview-view';
import {LocaleProvider} from './locale-context';
import type {PerspectiveProposal} from './perspective-proposal';
import type {OwnerAPI} from './owner-api';

const proposal={tenant_id:'tenant',project_id:'project',producer_task_id:'producer',result_id:'result',
 result:{sha256:'a'.repeat(64)},base:{source:{artifact_id:'source',sha256:'b'.repeat(64)}},
 canvas:{width:100,height:80}} as PerspectiveProposal;

afterEach(()=>{cleanup();vi.restoreAllMocks();});

it('invalidates perspective preview display when source or canvas changes under the same result',()=>{
 const source={...proposal,base:{...proposal.base,source:{...proposal.base.source,sha256:'c'.repeat(64)}}};
 const canvas={...proposal,canvas:{...proposal.canvas,width:101}};
 expect(perspectivePreviewDisplayScope(source)).not.toBe(perspectivePreviewDisplayScope(proposal));
 expect(perspectivePreviewDisplayScope(canvas)).not.toBe(perspectivePreviewDisplayScope(proposal));
});

it('aborts the old perspective preview request when source changes',()=>{
 const perspectivePreview=vi.fn((_proposal:PerspectiveProposal,_variant?:string,_signal?:AbortSignal)=>new Promise<never>(()=>{}));
 const api={perspectivePreview} as unknown as OwnerAPI;
 const view=render(<LocaleProvider><PerspectivePreviewView api={api} proposal={proposal}/></LocaleProvider>);
 const oldSignal=perspectivePreview.mock.calls[0]?.[2] as AbortSignal;
 const changed={...proposal,base:{...proposal.base,source:{...proposal.base.source,sha256:'c'.repeat(64)}}};
 view.rerender(<LocaleProvider><PerspectivePreviewView api={api} proposal={changed}/></LocaleProvider>);
 expect(oldSignal.aborted).toBe(true);
 expect(perspectivePreview).toHaveBeenCalledTimes(2);
 view.unmount();
});

it('retries unavailable preview and selects the larger variant',async()=>{
 const perspectivePreview=vi.fn().mockRejectedValue(new Error('not ready'));
 const api={perspectivePreview} as unknown as OwnerAPI;
 render(<LocaleProvider><PerspectivePreviewView api={api} proposal={proposal}/></LocaleProvider>);
 await screen.findByText('The result is unavailable. Retry after processing finishes.');
 await clickWhenEnabled(screen.getByRole('button',{name:'Reload perspective preview'}));
 await waitFor(()=>expect(perspectivePreview).toHaveBeenCalledTimes(2));
 await clickWhenEnabled(screen.getByRole('button',{name:'Show 2560px perspective preview'}));
 await waitFor(()=>expect(perspectivePreview.mock.calls[2]?.[1]).toBe('preview2560'));
});
