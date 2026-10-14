// @vitest-environment jsdom
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {cleanup,fireEvent,render,screen} from '@testing-library/react';
const mocks=vi.hoisted(()=>({params:{tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002'},push:vi.fn()}));
vi.mock('react-native',async()=>import('react-native-web'));
vi.mock('expo-router',()=>({router:{push:mocks.push},useLocalSearchParams:()=>mocks.params}));
vi.mock('./locale-context',()=>({useLocale:()=>({t:(key:string)=>key})}));
import Route from '../app/interchange/import-format';
afterEach(cleanup);beforeEach(()=>{mocks.push.mockReset();mocks.params={tenant:'00000000-0000-4000-8000-000000000001',project:'00000000-0000-4000-8000-000000000002'};});
it.each(['photoeditor_bundle','xmp','captureone_sidecar'])('passes %s and exact project scope to upload',format=>{
 render(<Route/>);expect(mocks.push).not.toHaveBeenCalled();fireEvent.click(screen.getByText(`interchange_format_${format}`));
 expect(mocks.push).toHaveBeenCalledWith({pathname:'/interchange/upload',params:{...mocks.params,format}});
});
it('rejects malformed route before navigation',()=>{
 mocks.params.tenant='invalid';render(<Route/>);expect(screen.getByRole('alert')).toBeTruthy();expect(mocks.push).not.toHaveBeenCalled();
});
