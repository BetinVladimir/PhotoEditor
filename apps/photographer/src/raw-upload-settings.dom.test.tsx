import {APIError} from '@photoeditor/api-client';
// @vitest-environment jsdom
import {afterEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
vi.mock('./locale-context',async()=>{const __factory=async()=>{const en=await import('../../../locales/en/common.json');return {useLocale:()=>({t:(key:keyof typeof en.default)=>en.default[key]})}};const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('react-native',async()=>import('react-native-web'));
import {RAWUploadSettings} from './raw-upload-settings';
import type {OwnerAPI} from './owner-api';
const project='11111111-1111-4111-8111-111111111111';
const catalogue={project_id:project,available:true,development:{decoder_version:'rawpy-0.27.1/libraw-0.21.4',demosaic:['ahd','dcb'],wb_source:['camera','daylight','auto'],highlight_policy:['clip','blend','reconstruct'],exposure_min:-2,exposure_max:3}};
afterEach(cleanup);
it('requires explicit selection and refuses invalid exposure',async()=>{
 const onChange=vi.fn();const api={rawCapabilities:vi.fn().mockResolvedValue(catalogue)} as unknown as OwnerAPI;
 render(<RAWUploadSettings api={api} project={project} disabled={false} onChange={onChange}/>);
 await screen.findByText('Use custom RAW settings');expect(onChange).toHaveBeenLastCalledWith(undefined,true);
 fireEvent.click(screen.getByText('Use custom RAW settings'));await waitFor(()=>expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({decoder_version:catalogue.development.decoder_version,exposure_ev:0}),true));
 fireEvent.click(screen.getByText('dcb'));await waitFor(()=>expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({demosaic:'dcb'}),true));
 fireEvent.change(screen.getByLabelText('RAW exposure EV'),{target:{value:'4'}});await waitFor(()=>expect(onChange).toHaveBeenLastCalledWith(undefined,false));
 fireEvent.click(screen.getByText('Use custom RAW settings'));await waitFor(()=>expect(onChange).toHaveBeenLastCalledWith(undefined,true));
});
it('does not advertise options when unavailable and aborts on unmount',async()=>{
 const rawCapabilities=vi.fn().mockResolvedValue({project_id:project,available:false});const onChange=vi.fn();
 const view=render(<RAWUploadSettings api={{rawCapabilities} as unknown as OwnerAPI} project={project} disabled={false} onChange={onChange}/>);
 await screen.findByText('Custom RAW settings are unavailable. Original import defaults remain available.');expect(screen.queryByText('Use custom RAW settings')).toBeNull();
 const signal=rawCapabilities.mock.calls[0][1] as AbortSignal;view.unmount();expect(signal.aborted).toBe(true);
});

it('propagates revoked RAW catalogue authority to the import scope',async()=>{
 const lost=vi.fn(),onChange=vi.fn(),api={rawCapabilities:vi.fn().mockRejectedValue(new APIError(403,'FORBIDDEN'))} as unknown as OwnerAPI;
 render(<RAWUploadSettings api={api} project={project} disabled={false} onChange={onChange} onAuthorityLost={lost}/>);
 await waitFor(()=>expect(lost).toHaveBeenCalledTimes(1));
});
