package interfaces

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

type YouTubeImportService interface {
	Import(ctx context.Context, url, language string) (*types.YouTubeImportResult, error)
	// ImportFromIM admits only a trusted IM principal to the admin-configured destination.
	ImportFromIM(ctx context.Context, url, language string) (*types.YouTubeImportResult, error)
}
