import {useLocale} from '../../src/locale-context';
import {Button, Card, HelperText, List, Text} from 'react-native-paper';
import {useMemo, useState} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors backend/internal/billing/api.go Catalog.
// Criteria E: prices never from route params.
type CatalogMoney = {currency: string; net_minor: string; gross_minor: string; tax_mode: string};
type PlanLimit = {resource_code: string; quantity: string};
type CatalogItem = {
  plan_code: string; name_key: string; custom: boolean;
  published: boolean; draft_only: boolean;
  price: CatalogMoney; limits: PlanLimit[];
  can_purchase: boolean; unavailable_reason?: string | null;
};
type Catalog = {catalog_revision: string; scope: string; currency: string; items: CatalogItem[]};

export default function BillingPlans() {
  const {t} = useLocale();
  const {tenant, currentPlan} = useLocalSearchParams<{tenant: string; currentPlan: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);
  const [selected, setSelected] = useState('');

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-catalog']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      return api.billingCatalog(signal) as Promise<Catalog>;
    },
  );

  const data = resource.data;
  // Custom plan is always hidden per spec.
  const publicItems = data?.items.filter(i => !i.custom && i.published) ?? [];

  const router = useRouter();
  const handleSelect = () => {
    if (!selected) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.push as any)({pathname: '/settings/billing/change-confirm', params: {tenant, planCode: selected}});
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_plans_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_plans_empty')}</Text>
        )}
        {publicItems.map(item => {
          const isCurrent = item.plan_code === currentPlan;
          const isComingSoon = !item.can_purchase && !isCurrent;
          return (
            <Card key={item.plan_code} style={{marginBottom: 8}}>
              <Card.Content>
                <List.Item
                  title={item.name_key}
                  onPress={item.can_purchase && !isCurrent ? () => setSelected(item.plan_code) : undefined}
                  right={() => selected === item.plan_code ? <Text style={s.meta}>✓</Text> : null}
                />
                {isCurrent && (
                  <Text style={s.meta}>{t('billing_plans_current')}</Text>
                )}
                {isComingSoon && (
                  <Text style={s.meta}>{t('billing_plans_coming_soon')}</Text>
                )}
                <Text style={s.meta}>
                  {item.price.net_minor} {item.price.currency.toUpperCase()} {t('billing_plans_net')}
                  {' / '}
                  {item.price.gross_minor} {t('billing_plans_gross')}
                </Text>
                {item.draft_only && (
                  <HelperText type="error" visible>
                    {t('billing_plans_draft_unavailable')}
                  </HelperText>
                )}
              </Card.Content>
            </Card>
          );
        })}
        {selected && selected !== currentPlan && (
          <Button mode="contained" onPress={handleSelect}>{t('billing_plans_select')}</Button>
        )}
      </View>
    </SafeAreaView>
  );
}
