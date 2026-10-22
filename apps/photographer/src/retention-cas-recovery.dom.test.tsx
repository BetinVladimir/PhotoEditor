// @vitest-environment jsdom
import {act,cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import en from '../../../locales/en/common.json';
import ru from '../../../locales/ru/common.json';
import bg from '../../../locales/bg/common.json';
const mocks=vi.hoisted(()=>({api:{tenantSettings:vi.fn(),updateTenant:vi.fn()},auth:{origin:'https://retention.test',client:()=>({})},canAdmin:true,locale:'en' as 'en'|'ru'|'bg'}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('./localized-router',()=>({useLocalSearchParams:()=>({tenant:'11111111-1111-4111-8111-111111111111'})}));
vi.mock('./auth-context',()=>({useAuth:()=>mocks.auth}));
vi.mock('./owner-api',()=>({OwnerAPI:class{constructor(){return mocks.api;}}}));
vi.mock('./workspace-access-context',()=>({useWorkspaceAccess:()=>({canAdmin:mocks.canAdmin})}));
vi.mock('./locale-context',async()=>{const __factory=()=>({useLocale:()=>({t:(key:keyof typeof en)=>({en,ru,bg})[mocks.locale][key]})});const __out=await __factory();const {withUILocaleDouble}=await import('./test/locale-double');return {...__out,useLocale:()=>withUILocaleDouble(__out.useLocale())};});
vi.mock('./use-review-foreground',()=>({useReviewForeground:()=>({active:true,generation:0})}));
import Retention from '../screens/settings/retention';
const original={id:'11111111-1111-4111-8111-111111111111',revision:'4',retention:{processing_days:7,exports_days:14,temporary_hours:24}};
beforeEach(()=>{vi.resetAllMocks();mocks.canAdmin=true;mocks.locale='en';mocks.api.tenantSettings.mockResolvedValue(original);});
afterEach(cleanup);
it('reconciles role withdrawal before another retention CAS and refuses the old reply',async()=>{
 let finishOld!:(value:unknown)=>void,finishNew!:(value:unknown)=>void;
 const current={...original,revision:'5',retention:{...original.retention,processing_days:9}};
 mocks.api.tenantSettings.mockResolvedValueOnce(original).mockResolvedValue(current);
 mocks.api.updateTenant.mockImplementationOnce(()=>new Promise(resolve=>{finishOld=resolve})).mockImplementationOnce(()=>new Promise(resolve=>{finishNew=resolve}));
 const view=render(<Retention/>);await screen.findByDisplayValue('7');
 fireEvent.change(screen.getByLabelText('Processing days (1–365)'),{target:{value:'8'}});fireEvent.click(screen.getByText('Save retention'));
 await waitFor(()=>expect(mocks.api.updateTenant).toHaveBeenCalledTimes(1));
 mocks.canAdmin=false;view.rerender(<Retention/>);
 expect((mocks.api.updateTenant.mock.calls[0][2] as AbortSignal).aborted).toBe(true);
 await screen.findByDisplayValue('9');fireEvent.click(screen.getByText('Save retention'));expect(mocks.api.updateTenant).toHaveBeenCalledTimes(1);
 mocks.canAdmin=true;view.rerender(<Retention/>);await screen.findByDisplayValue('9');
 fireEvent.change(screen.getByLabelText('Processing days (1–365)'),{target:{value:'10'}});fireEvent.click(screen.getByText('Save retention'));
 await waitFor(()=>expect(mocks.api.updateTenant).toHaveBeenCalledTimes(2));
 expect(mocks.api.updateTenant.mock.calls[1].slice(0,2)).toEqual([current,{retention:{processing_days:10,exports_days:14,temporary_hours:24}}]);
 await act(async()=>{finishOld({...original,revision:'5',retention:{...original.retention,processing_days:8}});});
 expect((mocks.api.updateTenant.mock.calls[1][2] as AbortSignal).aborted).toBe(false);expect(screen.getByText('Stop loading')).toBeTruthy();expect(screen.queryByDisplayValue('8')).toBeNull();
 await act(async()=>{finishNew({...current,revision:'6',retention:{...current.retention,processing_days:10}});});
 await screen.findByDisplayValue('10');expect(mocks.api.tenantSettings).toHaveBeenCalledTimes(3);
});
it('refuses out-of-range retention before writing',async()=>{
 render(<Retention/>);await screen.findByDisplayValue('7');
 fireEvent.change(screen.getByLabelText('Temporary hours (1–168)'),{target:{value:'169'}});fireEvent.click(screen.getByText('Save retention'));
 expect(mocks.api.updateTenant).not.toHaveBeenCalled();expect(screen.getByText('Retention values must be whole numbers within the displayed limits.')).toBeTruthy();
});

it('translates retention controls and an existing validation error when language changes',async()=>{
 const view=render(<Retention/>);await screen.findByDisplayValue('7');
 fireEvent.change(screen.getByRole('textbox',{name:en.retention_processing_days}),{target:{value:'0'}});
 fireEvent.click(screen.getByRole('button',{name:en.retention_save}));
 await screen.findByText(en.retention_invalid);
 for(const [locale,catalog] of [['ru',ru],['bg',bg],['en',en]] as const){
  mocks.locale=locale;view.rerender(<Retention/>);
  expect(screen.getByText(catalog.retention_title)).toBeTruthy();
  for(const name of [catalog.retention_processing_days,catalog.retention_exports_days,catalog.retention_temporary_hours])expect(screen.getByRole('textbox',{name})).toBeTruthy();
  expect(screen.getByRole('button',{name:catalog.retention_save})).toBeTruthy();
  expect(screen.getByRole('button',{name:catalog.retention_reload})).toBeTruthy();
  expect(screen.getByRole('alert').textContent).toContain(catalog.retention_invalid);
 }
 expect(mocks.api.updateTenant).not.toHaveBeenCalled();
});
