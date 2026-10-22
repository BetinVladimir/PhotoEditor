// @vitest-environment jsdom
import {afterEach,it,expect,vi} from 'vitest';
import {act,cleanup,render,waitFor} from '@testing-library/react';
import {useMemo} from 'react';
vi.mock('react-native',async()=>import('react-native-web'));
import {AuthProvider,useAuth} from './auth-context';
import {OwnerAuth} from './auth';
afterEach(()=>{cleanup();vi.restoreAllMocks()});
it('changes the auth facade and memoized client only at session replacement',async()=>{
 vi.spyOn(OwnerAuth.prototype,'restore').mockImplementation(async function(this:OwnerAuth){this.setTokens({access_token:'first',csrf_token:'first'});return true});
 let shown!:ReturnType<typeof useAuth>,client!:ReturnType<OwnerAuth['client']>;
 function Consumer(){const auth=useAuth();shown=auth;client=useMemo(()=>auth.client(),[auth]);return null}
 render(<AuthProvider><Consumer/></AuthProvider>);await waitFor(()=>expect(shown).toBeTruthy());
 const first=shown,firstClient=client;expect(first.client()).toBe(firstClient);
 act(()=>shown.setTokens({access_token:'second',csrf_token:'second'}));
 await waitFor(()=>expect(shown).not.toBe(first));expect(client).not.toBe(firstClient);
 expect(()=>first.client()).toThrow('Auth session changed');expect(shown.client()).toBe(client);
});
