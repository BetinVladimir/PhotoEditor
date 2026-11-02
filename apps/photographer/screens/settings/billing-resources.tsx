import {formatMoney as formatBillingMoney} from '../../src/billing-format';
import {useLocale} from '../../src/locale-context';
import {Button, HelperText, List, Text} from 'react-native-paper';
import {useMemo, useState} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors backend/internal/billing/api.go Quote/CatalogItem.
// Criteria E: prices never from route params; quantity comes from user input, max from server.
type QuoteMoney = {currency: string; net_minor: string; gross_minor: string; tax_minor: string};
type AddonOffer = {
  offer_code: string; resource_code: string; recurring: boolean;
  quantity_per_pack: string; max_packs: number;
  price_per_pack: QuoteMoney; expiry_days?: number | null;
};
type AddonQuote = {
  offers: AddonOffer[];
  subtotal: QuoteMoney;
  tax: QuoteMoney;
  total: QuoteMoney;
  valid_until: string;
};

export default function BillingResources() {
  const {t,locale} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);
  const [quantities, setQuantities] = useState<Record<string, number>>({});

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-addon-offers']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      return api.billingAddonOffers(signal) as Promise<AddonQuote>;
    },
  );

  const data = resource.data;
  const router = useRouter();

  const handleProceed = () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.push as any)({pathname: '/settings/billing/checkout', params: {tenant}});
  };

  const hasSelection = Object.values(quantities).some(q => q > 0);

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_addon_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('billing_addon_empty')}</Text>
        )}
        {data?.offers.map(offer => (
          <List.Item
            key={offer.offer_code}
            title={offer.resource_code}
            description={
              offer.recurring
                ? `${t('billing_addon_recurring_note')} · ${formatBillingMoney(offer.price_per_pack.gross_minor,offer.price_per_pack.currency,locale)}`
                : `${formatBillingMoney(offer.price_per_pack.gross_minor,offer.price_per_pack.currency,locale)}`
            }
            right={() => (
              <View>
                {offer.expiry_days && (
                  <Text style={s.meta}>{t('billing_addon_expiry')}: {offer.expiry_days}d</Text>
                )}
              </View>
            )}
          />
        ))}
        {data && (
          <>
            <List.Item
              title={`${t('billing_addon_subtotal')}: ${formatBillingMoney(data.subtotal.gross_minor,data.subtotal.currency,locale)}`}
              description={`${t('billing_addon_tax')}: ${formatBillingMoney(data.tax.gross_minor,data.tax.currency,locale)} / ${t('billing_addon_total')}: ${formatBillingMoney(data.total.gross_minor,data.total.currency,locale)}`}
            />
            {hasSelection && (
              <HelperText type="info" visible>
                {t('billing_addon_quantity')}
              </HelperText>
            )}
            <Button mode="contained" onPress={handleProceed} disabled={!hasSelection}>
              {t('billing_addon_proceed')}
            </Button>
          </>
        )}
      </View>
    </SafeAreaView>
  );
}
