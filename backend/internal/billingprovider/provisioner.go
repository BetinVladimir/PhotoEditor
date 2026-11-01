package billingprovider

import (
	"context"
	"fmt"
)

// CatalogItem describes a billing plan product to be provisioned.
type CatalogItem struct {
	// MappingKey is the immutable local identifier, e.g. "plan:pro:v1".
	MappingKey  string
	Name        string
	Description string
}

// PriceItem describes a price to be provisioned for a CatalogItem.
type PriceItem struct {
	// MappingKey is the immutable local key, e.g. "price:pro:monthly:v1".
	MappingKey  string
	ProductKey  string // references CatalogItem.MappingKey
	AmountMinor int64  // in smallest currency unit
	Currency    string // ISO 4217 lowercase, e.g. "eur"
	Interval    string // "month" or "year"
	IsTrial     bool   // trial items have no provider price (criteria C)
}

// ProvisionedPrice holds the mapping result for a price.
type ProvisionedPrice struct {
	MappingKey  string
	ProviderID  string // empty for trial prices
	AmountMinor int64
	Currency    string
	Interval    string
	Version     int
	IsTrial     bool
}

// ProvisionedProduct holds the mapping result for a product.
type ProvisionedProduct struct {
	MappingKey string
	ProviderID string
}

// MappingStore persists provider ID mappings for products and prices.
// Implementations may use a database or in-memory store.
type MappingStore interface {
	GetProductMapping(ctx context.Context, mappingKey string) (providerID string, found bool, err error)
	SetProductMapping(ctx context.Context, mappingKey, providerID string) error
	GetPriceMapping(ctx context.Context, mappingKey string) (ProvisionedPrice, bool, error)
	SetPriceMapping(ctx context.Context, mappingKey string, m ProvisionedPrice) error
}

// Provisioner syncs the local billing catalog to the provider.
// It is idempotent: running twice produces the same set of objects.
type Provisioner struct {
	provider Provider
	store    MappingStore
}

// NewProvisioner returns a Provisioner backed by the given provider and store.
func NewProvisioner(p Provider, s MappingStore) *Provisioner {
	return &Provisioner{provider: p, store: s}
}

// ProvisionResult holds the outcome of a full provisioning run.
type ProvisionResult struct {
	Products []ProvisionedProduct
	Prices   []ProvisionedPrice
}

// Run provisions all catalog items and their prices for the given account.
// catalogVersion is used as part of idempotency keys.
// Existing items are reused; changed prices create a new version.
func (pr *Provisioner) Run(ctx context.Context, catalogVersion, accountRef string, items []CatalogItem, prices []PriceItem) (ProvisionResult, error) {
	productMap := make(map[string]ProvisionedProduct, len(items))
	var result ProvisionResult

	for _, item := range items {
		pp, err := pr.provisionProduct(ctx, catalogVersion, accountRef, item)
		if err != nil {
			return ProvisionResult{}, fmt.Errorf("provision product %q: %w", item.MappingKey, err)
		}
		productMap[item.MappingKey] = pp
		result.Products = append(result.Products, pp)
	}

	for _, pi := range prices {
		pp, err := pr.provisionPrice(ctx, catalogVersion, accountRef, pi, productMap)
		if err != nil {
			return ProvisionResult{}, fmt.Errorf("provision price %q: %w", pi.MappingKey, err)
		}
		result.Prices = append(result.Prices, pp)
	}

	return result, nil
}

func (pr *Provisioner) provisionProduct(ctx context.Context, catalogVersion, accountRef string, item CatalogItem) (ProvisionedProduct, error) {
	if id, found, err := pr.store.GetProductMapping(ctx, item.MappingKey); err != nil {
		return ProvisionedProduct{}, err
	} else if found {
		return ProvisionedProduct{MappingKey: item.MappingKey, ProviderID: id}, nil
	}

	key := fmt.Sprintf("provision:product:%s:%s:%s", accountRef, catalogVersion, item.MappingKey)
	hash := fmt.Sprintf("%s|%s|%s", item.Name, item.Description, catalogVersion)
	prod, err := pr.provider.CreateProduct(ctx, key, hash, CreateProductParams{
		Name:        item.Name,
		Description: item.Description,
		Metadata:    map[string]string{"mapping_key": item.MappingKey, "catalog_version": catalogVersion},
	})
	if err != nil {
		return ProvisionedProduct{}, err
	}

	if err := pr.store.SetProductMapping(ctx, item.MappingKey, prod.ID); err != nil {
		return ProvisionedProduct{}, err
	}
	return ProvisionedProduct{MappingKey: item.MappingKey, ProviderID: prod.ID}, nil
}

func (pr *Provisioner) provisionPrice(ctx context.Context, catalogVersion, accountRef string, item PriceItem, products map[string]ProvisionedProduct) (ProvisionedPrice, error) {
	// Criteria C: trial prices have no provider price.
	if item.IsTrial {
		return ProvisionedPrice{MappingKey: item.MappingKey, IsTrial: true, AmountMinor: 0}, nil
	}

	existing, found, err := pr.store.GetPriceMapping(ctx, item.MappingKey)
	if err != nil {
		return ProvisionedPrice{}, err
	}
	if found {
		// Criteria D: verify immutable fields match.
		if existing.AmountMinor != item.AmountMinor || existing.Currency != item.Currency || existing.Interval != item.Interval {
			// Price changed → create new version with a versioned mapping key.
			return pr.createNewPriceVersion(ctx, catalogVersion, accountRef, item, products, existing.Version+1)
		}
		return existing, nil
	}

	return pr.createNewPriceVersion(ctx, catalogVersion, accountRef, item, products, 1)
}

func (pr *Provisioner) createNewPriceVersion(ctx context.Context, catalogVersion, accountRef string, item PriceItem, products map[string]ProvisionedProduct, version int) (ProvisionedPrice, error) {
	pp, ok := products[item.ProductKey]
	if !ok {
		return ProvisionedPrice{}, fmt.Errorf("product not provisioned for key %q", item.ProductKey)
	}

	versionedKey := fmt.Sprintf("%s:v%d", item.MappingKey, version)
	key := fmt.Sprintf("provision:price:%s:%s:%s", accountRef, catalogVersion, versionedKey)
	hash := fmt.Sprintf("%s|%d|%s|%s", pp.ProviderID, item.AmountMinor, item.Currency, item.Interval)
	price, err := pr.provider.CreatePrice(ctx, key, hash, CreatePriceParams{
		ProductID:   pp.ProviderID,
		AmountMinor: item.AmountMinor,
		Currency:    item.Currency,
		Interval:    item.Interval,
		Metadata:    map[string]string{"mapping_key": item.MappingKey, "version": fmt.Sprintf("%d", version)},
	})
	if err != nil {
		return ProvisionedPrice{}, err
	}

	m := ProvisionedPrice{
		MappingKey:  item.MappingKey,
		ProviderID:  price.ID,
		AmountMinor: item.AmountMinor,
		Currency:    item.Currency,
		Interval:    item.Interval,
		Version:     version,
	}
	if err := pr.store.SetPriceMapping(ctx, item.MappingKey, m); err != nil {
		return ProvisionedPrice{}, err
	}
	return m, nil
}

// InMemoryMappingStore is an in-memory MappingStore for testing.
type InMemoryMappingStore struct {
	products map[string]string
	prices   map[string]ProvisionedPrice
}

// NewInMemoryMappingStore returns a ready-to-use InMemoryMappingStore.
func NewInMemoryMappingStore() *InMemoryMappingStore {
	return &InMemoryMappingStore{
		products: make(map[string]string),
		prices:   make(map[string]ProvisionedPrice),
	}
}

func (s *InMemoryMappingStore) GetProductMapping(_ context.Context, mappingKey string) (string, bool, error) {
	id, ok := s.products[mappingKey]
	return id, ok, nil
}

func (s *InMemoryMappingStore) SetProductMapping(_ context.Context, mappingKey, providerID string) error {
	s.products[mappingKey] = providerID
	return nil
}

func (s *InMemoryMappingStore) GetPriceMapping(_ context.Context, mappingKey string) (ProvisionedPrice, bool, error) {
	m, ok := s.prices[mappingKey]
	return m, ok, nil
}

func (s *InMemoryMappingStore) SetPriceMapping(_ context.Context, mappingKey string, m ProvisionedPrice) error {
	s.prices[mappingKey] = m
	return nil
}
