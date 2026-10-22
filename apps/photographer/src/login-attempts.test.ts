import {expect,it,vi} from 'vitest';
import {LoginAttempts} from './login-attempts';
it('retains exact request and verify keys across uncertainty but changes keys for changed inputs',()=>{
 const attempts=new LoginAttempts('native'),request=attempts.requestAttempt('a@example.test','en'),verify=attempts.verifyAttempt('challenge','123456');
 expect(attempts.requestAttempt('a@example.test','en')).toEqual(request);expect(attempts.verifyAttempt('challenge','123456')).toEqual(verify);
 expect(attempts.verifyAttempt('challenge','654321').key).not.toBe(verify.key);expect(attempts.requestAttempt('b@example.test','en').key).not.toBe(request.key);
 const returned=attempts.requestAttempt('b@example.test','en');returned.body.email='tampered';expect(attempts.requestAttempt('b@example.test','en').body.email).toBe('b@example.test');
});
it('binds both native operations to one random 32-byte nonce without bootstrap network access',async()=>{
 const attempts=new LoginAttempts('native'),read=vi.fn(),signal=new AbortController().signal;
 const first=await attempts.headers(read,signal);expect(first['X-Photo-Auth-Nonce']).toMatch(/^[A-Za-z0-9_-]{43}$/);expect(await attempts.headers(read,signal)).toEqual(first);expect(read).not.toHaveBeenCalled();
});
it('retains exact web bootstrap header across retry instead of rotating its receipt scope',async()=>{
 const attempts=new LoginAttempts('web'),read=vi.fn().mockResolvedValue({csrf_token:'frozen-bootstrap'}),signal=new AbortController().signal;
 expect(await attempts.headers(read,signal)).toEqual({'X-CSRF-Token':'frozen-bootstrap'});expect(await attempts.headers(read,signal)).toEqual({'X-CSRF-Token':'frozen-bootstrap'});expect(read).toHaveBeenCalledOnce();
 const prior=attempts.requestAttempt('a','en');attempts.newChallenge();expect(attempts.requestAttempt('a','en').key).not.toBe(prior.key);
});
it('refreshes expired web bootstrap only for an explicit new challenge with new keys',async()=>{
 const attempts=new LoginAttempts('web'),read=vi.fn().mockResolvedValueOnce({csrf_token:'old-bootstrap'}).mockResolvedValueOnce({csrf_token:'new-bootstrap'}),signal=new AbortController().signal;
 const old=attempts.requestAttempt('a','en');await attempts.headers(read,signal);expect(await attempts.headers(read,signal)).toEqual({'X-CSRF-Token':'old-bootstrap'});
 attempts.newChallenge();expect(attempts.requestAttempt('a','en').key).not.toBe(old.key);expect(await attempts.headers(read,signal)).toEqual({'X-CSRF-Token':'new-bootstrap'});expect(read).toHaveBeenCalledTimes(2);
});
