package access

import (
	"context"
	"github.com/Tencent/WeKnora/internal/types"
)

// MessageFileLookup loads a session-authorized persisted message.
type MessageFileLookup interface {
	GetMessage(context.Context, string, string) (*types.Message, error)
}
