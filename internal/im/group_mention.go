package im

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/types"
)

func (s *Service) shouldIgnoreUnmentionedGroupMessage(ctx context.Context, msg *IncomingMessage, channel *IMChannel, adapter Adapter) (bool, error) {
	if msg.ChatType != ChatTypeGroup || (msg.Platform != PlatformFeishu && msg.Platform != PlatformLark) {
		return false, nil
	}
	smart := channel.AgentID == types.BuiltinSmartReasoningID
	if !smart && channel.AgentID != "" {
		agent, err := s.agentService.GetAgentByID(withIMIdentity(ctx, channel, msg), channel.AgentID)
		if err != nil {
			return true, fmt.Errorf("resolve group agent: %w", err)
		}
		smart = agent != nil && agent.Config.AgentMode == types.AgentModeSmartReasoning
	}
	if !smart {
		return false, nil
	}
	checker, ok := adapter.(interface {
		IsBotMentioned(context.Context, *IncomingMessage) (bool, error)
	})
	if !ok {
		return true, nil
	}
	mentioned, err := checker.IsBotMentioned(ctx, msg)
	return !mentioned, err
}
