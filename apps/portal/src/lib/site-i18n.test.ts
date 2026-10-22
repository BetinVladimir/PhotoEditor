import {expect,it} from 'vitest';
import {siteMessages,statusLabel} from './site-i18n';
it.each(['ru','en','bg'] as const)('keeps known statuses translated and unknown names safe in %s',locale=>{
 expect(statusLabel(locale,'queued')).toBe(siteMessages[locale].status_queued);
 for(const status of ['constructor','__proto__','toString','not-a-status'])expect(statusLabel(locale,status)).toBe(siteMessages[locale].status_unknown);
});
