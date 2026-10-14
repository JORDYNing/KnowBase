package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

type agentWebSearchRepo struct {
	byIDTenant      uint64
	byID            string
	defaultTenant   uint64
	explicit        *types.WebSearchProviderEntity
	defaultProvider *types.WebSearchProviderEntity
}

func (r *agentWebSearchRepo) Create(context.Context, *types.WebSearchProviderEntity) error {
	return nil
}

func (r *agentWebSearchRepo) GetByID(_ context.Context, tenantID uint64, id string) (*types.WebSearchProviderEntity, error) {
	r.byIDTenant = tenantID
	r.byID = id
	return r.explicit, nil
}

func (r *agentWebSearchRepo) GetDefault(_ context.Context, tenantID uint64) (*types.WebSearchProviderEntity, error) {
	r.defaultTenant = tenantID
	return r.defaultProvider, nil
}

func (r *agentWebSearchRepo) List(context.Context, uint64) ([]*types.WebSearchProviderEntity, error) {
	return nil, nil
}

func (r *agentWebSearchRepo) Update(context.Context, *types.WebSearchProviderEntity) error {
	return nil
}

func (r *agentWebSearchRepo) Delete(context.Context, uint64, string) error { return nil }

func (r *agentWebSearchRepo) ClearDefault(context.Context, uint64, string) error { return nil }
