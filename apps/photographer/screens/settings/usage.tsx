import {useLocale} from '../../src/locale-context';
import {Banner, Card, List, ProgressBar, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors backend/internal/billing/api.go UsageSummaryResponse.
// Criteria E: prices never from route params.
type StorageCategories = {original: string; result: string; preview: string; archive: string};
type StorageBalance = {
  included: string; additional: string; total: string;
  committed: string; reserved: string; free: string;
  over_capacity: boolean; categories: StorageCategories; as_of: string;
};
type ResourceBalance = {
  resource_code: string; unit: string;
  granted: string; consumed: string; allocated: string;
  available: string; overrun: string; blocked: boolean;
};
type UsageSummary = {
  balances: ResourceBalance[];
  storage: StorageBalance;
  as_of: string;
  summary_revision: string;
};

export default function Usage() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'billing-usage']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      return api.billingUsage(signal) as Promise<UsageSummary>;
    },
  );

  const data = resource.data;
  const storage = data?.storage;
  const photoBalance = data?.balances.find(b => b.resource_code === 'standard_photo');
  const egressBalance = data?.balances.find(b => b.resource_code === 'egress');
  const operationBalances = data?.balances.filter(
    b => b.resource_code !== 'standard_photo' && b.resource_code !== 'egress' && b.resource_code !== 'storage'
  ) ?? [];

  const fmtBigInt = (v: string | undefined) => v ?? '0';

  const storageProgress = storage
    ? Math.min(1, Number(storage.committed) / Math.max(1, Number(storage.total)))
    : 0;

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('billing_usage_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {data?.as_of && (
          <Text style={s.meta}>{t('billing_usage_updated_at')}: {data.as_of}</Text>
        )}
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('settings_quotas_empty')}</Text>
        )}

        {/* Photos */}
        {photoBalance && (
          <Card>
            <Card.Content>
              <Text variant="titleMedium" style={s.name}>{t('billing_usage_photos')}</Text>
              <List.Item
                title={`${t('billing_usage_used')}: ${fmtBigInt(photoBalance.consumed)}`}
                description={`${t('billing_usage_admitted')}: ${fmtBigInt(photoBalance.allocated)}`}
              />
              <List.Item
                title={`${t('billing_usage_available')}: ${fmtBigInt(photoBalance.available)}`}
                description={photoBalance.overrun !== '0'
                  ? `${t('billing_usage_overrun')}: ${fmtBigInt(photoBalance.overrun)}`
                  : undefined}
              />
            </Card.Content>
          </Card>
        )}

        {/* Operations */}
        {operationBalances.length > 0 && (
          <Card>
            <Card.Content>
              <Text variant="titleMedium" style={s.name}>{t('billing_usage_operations')}</Text>
              {operationBalances.map(b => (
                <List.Item
                  key={b.resource_code}
                  title={b.resource_code}
                  description={`${t('billing_usage_available')}: ${fmtBigInt(b.available)}`}
                />
              ))}
            </Card.Content>
          </Card>
        )}

        {/* Storage */}
        {storage && (
          <Card>
            <Card.Content>
              <Text variant="titleMedium" style={s.name}>{t('billing_usage_storage_section')}</Text>
              <ProgressBar progress={storageProgress} />
              {storage.over_capacity && (
                <Text style={s.error}>{t('billing_usage_over_capacity')}</Text>
              )}
              <List.Item
                title={`${t('billing_usage_included')}: ${fmtBigInt(storage.included)}`}
                description={`${t('billing_usage_additional')}: ${fmtBigInt(storage.additional)}`}
              />
              <List.Item
                title={`${t('billing_usage_total')}: ${fmtBigInt(storage.total)}`}
                description={`${t('billing_usage_committed')}: ${fmtBigInt(storage.committed)} / ${t('billing_usage_free')}: ${fmtBigInt(storage.free)}`}
              />
              <Text variant="titleSmall" style={s.meta}>{t('billing_usage_original')}: {fmtBigInt(storage.categories.original)}</Text>
              <Text style={s.meta}>{t('billing_usage_result')}: {fmtBigInt(storage.categories.result)}</Text>
              <Text style={s.meta}>{t('billing_usage_preview')}: {fmtBigInt(storage.categories.preview)}</Text>
              <Text style={s.meta}>{t('billing_usage_archive')}: {fmtBigInt(storage.categories.archive)}</Text>
            </Card.Content>
          </Card>
        )}

        {/* Egress */}
        {egressBalance && (
          <Card>
            <Card.Content>
              <Text variant="titleMedium" style={s.name}>{t('billing_usage_egress')}</Text>
              <List.Item
                title={`${t('billing_usage_given')}: ${fmtBigInt(egressBalance.consumed)}`}
                description={`${t('billing_usage_allocated')}: ${fmtBigInt(egressBalance.allocated)} / ${t('billing_usage_available')}: ${fmtBigInt(egressBalance.available)}`}
              />
            </Card.Content>
          </Card>
        )}

        {data && !photoBalance && !storage && operationBalances.length === 0 && !egressBalance && (
          <Banner visible actions={[]}>{t('billing_usage_stale')}</Banner>
        )}
      </View>
    </SafeAreaView>
  );
}
