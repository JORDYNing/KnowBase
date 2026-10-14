package service

import (
	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

// agentRequiresRerankModel matches the retrieval tools and KB selection used at runtime.
func agentRequiresRerankModel(agent *types.CustomAgent) bool {
	if agent == nil || agent.Config.KBSelectionMode == "none" {
		return false
	}
	allowed := agent.Config.AllowedTools
	if len(allowed) == 0 {
		allowed = tools.DefaultAllowedTools()
	}
	for _, tool := range allowed {
		if tools.SuccessorToolName(tool) == tools.ToolSearchKnowledge {
			return true
		}
	}
	return false
}
