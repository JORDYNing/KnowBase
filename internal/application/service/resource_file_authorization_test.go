package service

import (
	"context"
	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

type isolatedFileCatalog struct{ tenant uint64 }

func (c isolatedFileCatalog) ResolvePath(context.Context, string) (string, *types.StoredResource, error) {
	return "local://2/exports/file.png", &types.StoredResource{TenantID: c.tenant, PhysicalPath: "local://2/exports/file.png"}, nil
}
func TestMessageCatalogRejectsForeignResourcesEvenWhenReferenced(t *testing.T) {
	ctx := types.WithCaller(context.Background(), types.Caller{TenantID: 1})
	ref := "resource://AbCdEfGhIjKlMnOpQrStUv"
	msg := &types.Message{Role: "assistant", AgentTenantID: 1, Content: ref}
	_, err := access.AuthorizeMessageFile(ctx, msg, ref, isolatedFileCatalog{tenant: 2})
	require.ErrorIs(t, err, access.ErrForbidden)
}
