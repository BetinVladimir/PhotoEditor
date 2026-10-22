// @vitest-environment jsdom
vi.mock('expo-router',async()=> (await import('./test/locale-router')).localeRouterFixture);
import {afterEach,expect,it,vi} from 'vitest';
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('react-native',async()=>import('react-native-web'));
import {MultiQualityView} from './multi-quality-view';
import {LocaleProvider} from './locale-context';
import type {MultiProposal} from './multi-proposal';
import type {OwnerAPI} from './owner-api';
import type {MultiIntent} from './multi-intent';
// UI transport failure fixtures only; do not establish contract/codec validity.
const proposal={tenant_id:'tenant',project_id:'project',operation:'optical.panorama',producer_task_id:'producer',result_id:'result',result:{sha256:'a'.repeat(64)}} as MultiProposal;
const intent={} as MultiIntent;
afterEach(()=>{cleanup();vi.restoreAllMocks();});

it('retries unavailable quality and aborts pending read on unmount',async()=>{
 const multiQuality=vi.fn().mockRejectedValue(new Error('pending'));
 const view=render(<LocaleProvider><MultiQualityView api={{multiQuality}} proposal={proposal} intent={intent}/></LocaleProvider>);
 await screen.findByText('Quality report unavailable; try again.');
 fireEvent.click(screen.getByRole('button',{name:'Refresh quality report'}));await waitFor(()=>expect(multiQuality).toHaveBeenCalledTimes(2));
 view.unmount();expect((multiQuality.mock.calls[1][2] as AbortSignal).aborted).toBe(true);
});
it('aborts previous scope and ignores its late report',async()=>{
 let finish!:(v:unknown)=>void;
 const multiQuality=vi.fn((_p:MultiProposal,_i:MultiIntent,_s?:AbortSignal)=>new Promise<unknown>(resolve=>{finish=resolve;}));const api={multiQuality} as unknown as Pick<OwnerAPI,'multiQuality'>;
 const view=render(<LocaleProvider><MultiQualityView api={api} proposal={proposal} intent={intent}/></LocaleProvider>);
 const oldFinish=finish,signal=multiQuality.mock.calls[0]?.[2] as AbortSignal;
 view.rerender(<LocaleProvider><MultiQualityView api={api} proposal={{...proposal,result_id:'new-result'}} intent={intent}/></LocaleProvider>);
 expect(signal.aborted).toBe(true);
 await act(async()=>{oldFinish({quality:{status:'pass',reasons:[],technical:{clipped_black_fraction:0,clipped_white_fraction:0}}});});
 expect(screen.queryByText('Technical checks passed')).toBeNull();
});
