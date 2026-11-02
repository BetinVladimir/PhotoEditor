import {useLocale} from '../../src/locale-context';
import {Button, Card, List, Text} from 'react-native-paper';
import {useMemo, useState} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

import type {BillingCatalog as Catalog} from '../../src/billing-catalog-response';
import {formatMoney} from '../../src/billing-format';

export default function BillingPlans() {
  const {t,locale} = useLocale();
  const {tenant, currentPlan} = useLocalSearchParams<{tenant: string; currentPlan: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);
  const [selected, setSelected] = useState('');

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, locale, 'billing-catalog']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      return api.billingCatalog(signal,locale) as Promise<Catalog>;
    },
  );

  const data = resource.data;
  // Custom plan is always hidden per spec.
  const publicItems = data?.plans.filter(i => i.code!=='custom') ?? [];

  const selectedPlan=publicItems.find(item=>item.code===selected&&item.can_purchase&&item.code!=='studio_pro'&&item.code!==currentPlan);
  const router = useRouter();
  const handleSelect = () => {
    if (!selectedPlan||resource.phase!=='ready') return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.push as any)({pathname: '/settings/billing/change-confirm', params: {tenant, planCode: selected}});
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_plans_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {data && publicItems.length===0 && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_plans_empty')}</Text>
        )}
        {publicItems.map(item => {
          const isCurrent = item.code === currentPlan;
          const eligible=item.can_purchase&&item.code!=='studio_pro'&&resource.phase==='ready';
          const isComingSoon = (!item.can_purchase||item.code==='studio_pro') && !isCurrent;
          return (
            <Card key={item.code} style={{marginBottom: 8}}>
              <Card.Content>
                <List.Item
                  title={item.label}
                  onPress={eligible && !isCurrent ? () => setSelected(item.code) : undefined}
                  right={() => selected === item.code ? <Text style={s.meta}>✓</Text> : null}
                />
                {isCurrent && (
                  <Text style={s.meta}>{t('billing_plans_current')}</Text>
                )}
                {isComingSoon && (
                  <Text style={s.meta}>{t('billing_plans_coming_soon')}</Text>
                )}
                <Text style={s.meta}>
                  {formatMoney(item.price.net_minor,item.price.currency,locale)} {t('billing_plans_net')}
                  {' / '}
                  {formatMoney(item.price.gross_minor,item.price.currency,locale)} {t('billing_plans_gross')}
                </Text>

              </Card.Content>
            </Card>
          );
        })}
        {selectedPlan && resource.phase==='ready' && (
          <Button mode="contained" onPress={handleSelect}>{t('billing_plans_select')}</Button>
        )}
      </View>
    </SafeAreaView>
  );
}
