// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';

const mocks=vi.hoisted(()=>({
 params:{tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002',snapshot:'00000000-0000-4000-8000-000000000003'},
 push:vi.fn(),
}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({router:{push:mocks.push},useLocalSearchParams:()=>mocks.params}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import Route from '../app/interchange/format';

afterEach(cleanup);
beforeEach(()=>{
 mocks.push.mockReset();
 mocks.params={tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002',snapshot:'00000000-0000-4000-8000-000000000003'};
});

it('sends one exact export route and permits a new snapshot after remount',()=>{
 const view=render(<Route/>);
 fireEvent.click(screen.getByText('interchange_format_xmp'));
 fireEvent.click(screen.getByText('interchange_format_captureone_sidecar'));
 expect(mocks.push).toHaveBeenCalledTimes(1);
 expect(mocks.push).toHaveBeenLastCalledWith({pathname:'/interchange/new',params:{
  tenant:mocks.params.tenant,project:mocks.params.project,direction:'export',format:'xmp',snapshot:mocks.params.snapshot,
 }});
 mocks.params={...mocks.params,snapshot:'00000000-0000-4000-8000-000000000004'};
 view.rerender(<Route/>);
 fireEvent.click(screen.getByText('interchange_format_captureone_sidecar'));
 expect(mocks.push).toHaveBeenCalledTimes(2);
 expect(mocks.push).toHaveBeenLastCalledWith({pathname:'/interchange/new',params:{
  tenant:mocks.params.tenant,project:mocks.params.project,direction:'export',format:'captureone_sidecar',snapshot:mocks.params.snapshot,
 }});
});

it('refuses malformed scope without navigation',()=>{
 mocks.params.snapshot='invalid';
 render(<Route/>);
 expect(screen.getByRole('alert')).toBeTruthy();
 expect(mocks.push).not.toHaveBeenCalled();
});
