package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// applyRequestReasoningEffort only changes runtime options, never the saved agent.
// Empty requests preserve both the graded default and legacy Thinking boolean.
func applyRequestReasoningEffort(override string, thinking **bool, effort *string) {
	level, ok := api.ParseReasoningEffort(override)
	if !ok || level == "" {
		return
	}
	enabled := level.Enabled()
	*thinking = &enabled
	*effort = string(level)
}

// ---------------------------------------------------------------------------
// Shared QA helpers: KB resolution, model resolution, retrieval tenant
// ---------------------------------------------------------------------------

// resolveKnowledgeBases resolves the effective knowledge base IDs and knowledge IDs
// for a QA request. Priority:
//  1. Explicit @mentions (request-specified kbIDs / knowledgeIDs)
//  2. RetrieveKBOnlyWhenMentioned -> disable KB if no mention
//  3. Agent's configured knowledge bases (via KBSelectionMode)
func (s *sessionService) resolveKnowledgeBases(
	ctx context.Context,
	req *types.QARequest,
) (kbIDs []string, knowledgeIDs []string, err error) {
	kbIDs = req.KnowledgeBaseIDs
	knowledgeIDs = req.KnowledgeIDs
	requestedKBIDs := append([]string(nil), req.KnowledgeBaseIDs...)
	customAgent := req.CustomAgent

	hasExplicitMention := len(kbIDs) > 0 || len(knowledgeIDs) > 0 || len(req.TagScopes) > 0
	if customAgent != nil {
		logger.Infof(ctx, "KB resolution: hasExplicitMention=%v, RetrieveKBOnlyWhenMentioned=%v, KBSelectionMode=%s",
			hasExplicitMention, customAgent.Config.RetrieveKBOnlyWhenMentioned, customAgent.Config.KBSelectionMode)
	}

	if hasExplicitMention {
		logger.Infof(ctx, "Using request-specified targets: kbs=%v, docs=%v", kbIDs, knowledgeIDs)

	} else if customAgent != nil && customAgent.Config.RetrieveKBOnlyWhenMentioned {
		kbIDs = nil
		knowledgeIDs = nil
		if anchor := s.questionOriginAnchor(ctx, customAgent, req); anchor != "" {
			// Picking a suggestion generated from a base the agent may read
			// selects that base, as an @mention would.
			kbIDs = []string{anchor}
			logger.Infof(ctx, "RetrieveKBOnlyWhenMentioned: retrieving from the picked suggestion's knowledge base %s",
				secutils.SanitizeForLog(anchor))
		} else {
			logger.Infof(ctx, "RetrieveKBOnlyWhenMentioned is enabled and no @ mention found, "+
				"KB retrieval disabled for this request")
		}
	} else if customAgent != nil {
		kbIDs = s.resolveKnowledgeBasesFromAgent(ctx, customAgent, req.Session.TenantID)
	}

	if err := types.AuthorizeTenantAPIKeyKnowledgeTargets(ctx, requestedKBIDs, req.KnowledgeIDs); err != nil {
		return nil, nil, err
	}
	kbIDs, err = types.FilterKnowledgeBasesForTenantAPIKeyScope(ctx, requestedKBIDs, kbIDs)
	if err != nil {
		return nil, nil, err
	}
	return kbIDs, knowledgeIDs, nil
}

// questionOriginAnchor returns the knowledge base a picked suggested question
// came from when the agent is allowed to read it, or "" otherwise. It lets a
// suggestion work for an agent that retrieves only on @mention, without
// reaching any base outside the agent's configured scope.
func (s *sessionService) questionOriginAnchor(
	ctx context.Context, agent *types.CustomAgent, req *types.QARequest,
) string {
	if req.QuestionOrigin == nil || req.Session == nil {
		return ""
	}
	kbID := strings.TrimSpace(req.QuestionOrigin.KnowledgeBaseID)
	if kbID == "" || !slices.Contains(s.resolveKnowledgeBasesFromAgent(ctx, agent, req.Session.TenantID), kbID) {
		return ""
	}
	return kbID
}

// resolveChatModelID resolves the effective chat model ID for a QA request.
//
// When a user-configured agent is selected, its model configuration must be
// complete and valid. The internal wiki fixer is the one exception: it is not
// exposed in the agent UI, so an empty model_id falls back to KB/system model
// selection. A request-level override may choose another valid model for this
// request, but it must not make an unconfigured or stale agent appear usable.
//
// Without an agent, the legacy KB / session / system fallback remains
// available for non-agent callers.
func (s *sessionService) resolveChatModelID(
	ctx context.Context,
	req *types.QARequest,
	knowledgeBaseIDs []string,
	knowledgeIDs []string,
) (string, error) {
	summaryModelID := req.SummaryModelID
	customAgent := req.CustomAgent
	session := req.Session
	configuredAgentModelID := ""

	if customAgent != nil {
		configuredAgentModelID = strings.TrimSpace(customAgent.Config.ModelID)
		if configuredAgentModelID == "" && customAgent.ID != types.BuiltinWikiFixerID {
			return "", fmt.Errorf("chat model is not configured: please set model_id on agent %s", customAgent.ID)
		}
		if configuredAgentModelID != "" {
			model, err := s.modelService.GetModelByID(ctx, configuredAgentModelID)
			if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA {
				return "", fmt.Errorf("configured chat model %s is unavailable for agent %s", configuredAgentModelID, customAgent.ID)
			}
		} else {
			// The wiki fixer is an internal agent and is intentionally omitted
			// from the agent-management list. It therefore cannot receive a
			// user-configured model_id; resolve it from the current Wiki KB or
			// the normal system KnowledgeQA fallback below instead.
			logger.Infof(ctx, "No model_id configured for internal wiki fixer %s, using KB/system fallback", customAgent.ID)
		}
	}

	summaryModelID = strings.TrimSpace(summaryModelID)
	if summaryModelID != "" {
		if model, err := s.modelService.GetModelByID(ctx, summaryModelID); err == nil && model != nil &&
			model.Type == types.ModelTypeKnowledgeQA {
			logger.Infof(ctx, "Using request's summary model override: %s", summaryModelID)
			return summaryModelID, nil
		}
		logger.Warnf(ctx, "Request provided invalid summary model ID %s, falling back", summaryModelID)
	}
	if configuredAgentModelID != "" {
		logger.Infof(ctx, "Using custom agent's model_id: %s", configuredAgentModelID)
		return configuredAgentModelID, nil
	}
	return s.selectChatModelID(ctx, session, knowledgeBaseIDs, knowledgeIDs)
}

// resolveRetrievalTenantID determines the tenant ID to use for retrieval scope.
// The authenticated caller takes precedence; trusted internal work uses its session tenant.
func (s *sessionService) resolveRetrievalTenantID(ctx context.Context, req *types.QARequest) uint64 {
	if tenant := types.CallerFromContext(ctx).TenantID; tenant != 0 {
		return tenant
	}
	if req != nil && req.Session != nil {
		return req.Session.TenantID
	}
	return 0
}

// validateQAWorkspace prevents agent/model execution from selecting a foreign workspace.
func validateQAWorkspace(ctx context.Context, req *types.QARequest) error {
	if req == nil || req.Session == nil || req.Session.TenantID == 0 {
		return apperrors.NewBadRequestError("session workspace is required")
	}
	caller := types.CallerFromContext(ctx)
	if caller.TenantID != 0 && caller.TenantID != req.Session.TenantID {
		return apperrors.NewForbiddenError("session does not belong to caller workspace")
	}
	if req.CustomAgent != nil && req.CustomAgent.TenantID != req.Session.TenantID {
		return apperrors.NewForbiddenError("agent does not belong to session workspace")
	}
	return nil
}

// applyAgentOverridesToChatManage applies custom agent configuration overrides
// to a ChatManage object that was initialized with system defaults.
// This covers: system prompt, context template, temperature, max tokens, thinking,
// citation output, retrieval thresholds, rewrite settings, fallback settings, FAQ strategy,
// and history turns.
func (s *sessionService) applyAgentOverridesToChatManage(
	ctx context.Context,
	customAgent *types.CustomAgent,
	cm *types.ChatManage,
) {
	if customAgent == nil {
		return
	}

	// Ensure defaults are set
	customAgent.EnsureDefaults()

	// Resolve inherited templates at request time; saved custom text remains authoritative.
	systemPrompt, contextTemplate := s.cfg.ResolveCustomAgentPrompts(customAgent)
	if systemPrompt != "" {
		cm.SummaryConfig.Prompt = systemPrompt
		logger.Infof(ctx, "Using custom agent's system_prompt")
	}
	if contextTemplate != "" {
		cm.SummaryConfig.ContextTemplate = contextTemplate
		logger.Infof(ctx, "Using custom agent's context_template")
	}
	if customAgent.Config.Temperature >= 0 {
		cm.SummaryConfig.Temperature = customAgent.Config.Temperature
		logger.Infof(ctx, "Using custom agent's temperature: %f", customAgent.Config.Temperature)
	}
	if customAgent.Config.MaxCompletionTokens > 0 {
		cm.SummaryConfig.MaxCompletionTokens = customAgent.Config.MaxCompletionTokens
		logger.Infof(ctx, "Using custom agent's max_completion_tokens: %d", customAgent.Config.MaxCompletionTokens)
	}
	// Agent-level thinking setting takes full control (no global fallback).
	// EnsureDefaults pins nil to explicit false so thinking_control wire formats
	// always receive a value.
	cm.SummaryConfig.Thinking = customAgent.Config.Thinking
	cm.SummaryConfig.ReasoningEffort = customAgent.Config.ReasoningEffort
	cm.CitationEnabled = customAgent.Config.CitationEnabled
	if customAgent.Config.Thinking != nil {
		logger.Infof(ctx, "Using custom agent's thinking: %v", *customAgent.Config.Thinking)
	} else {
		logger.Warnf(ctx, "Custom agent thinking is unset after EnsureDefaults; model thinking param will be omitted")
	}

	// Override retrieval strategy settings
	if customAgent.Config.EmbeddingTopK > 0 {
		cm.EmbeddingTopK = customAgent.Config.EmbeddingTopK
	}
	if customAgent.Config.KeywordThreshold > 0 {
		cm.KeywordThreshold = customAgent.Config.KeywordThreshold
	}
	if customAgent.Config.VectorThreshold > 0 {
		cm.VectorThreshold = customAgent.Config.VectorThreshold
	}
	if customAgent.Config.RerankTopK > 0 {
		cm.RerankTopK = customAgent.Config.RerankTopK
	}
	cm.RerankThreshold = customAgent.Config.RerankThreshold
	if customAgent.Config.RerankModelID != "" {
		cm.RerankModelID = customAgent.Config.RerankModelID
	}

	// Override rewrite settings
	cm.EnableRewrite = customAgent.Config.EnableRewrite
	cm.EnableQueryExpansion = customAgent.Config.EnableQueryExpansion
	if customAgent.Config.RewritePromptSystem != "" {
		cm.RewritePromptSystem = customAgent.Config.RewritePromptSystem
	}
	if customAgent.Config.RewritePromptUser != "" {
		cm.RewritePromptUser = customAgent.Config.RewritePromptUser
	}
	if customAgent.Config.QueryUnderstandModelID != "" {
		cm.QueryUnderstandModelID = customAgent.Config.QueryUnderstandModelID
		logger.Infof(ctx, "Using custom agent's query_understand_model_id: %s",
			customAgent.Config.QueryUnderstandModelID)
	}

	// Override fallback settings
	if customAgent.Config.FallbackStrategy != "" {
		cm.FallbackStrategy = types.FallbackStrategy(customAgent.Config.FallbackStrategy)
	}
	if customAgent.Config.FallbackResponse != "" {
		cm.FallbackResponse = customAgent.Config.FallbackResponse
	}
	if customAgent.Config.FallbackPrompt != "" {
		cm.FallbackPrompt = customAgent.Config.FallbackPrompt
	}

	// Override web search settings
	if customAgent.Config.WebSearchMaxResults > 0 {
		cm.WebSearchMaxResults = customAgent.Config.WebSearchMaxResults
	}

	// Override history turns
	if customAgent.Config.HistoryTurns > 0 {
		cm.MaxRounds = customAgent.Config.HistoryTurns
		logger.Infof(ctx, "Using custom agent's history_turns: %d", cm.MaxRounds)
	}
	if !customAgent.Config.MultiTurnEnabled {
		cm.MaxRounds = 0
		logger.Infof(ctx, "Multi-turn disabled by custom agent, clearing history")
	}

	// FAQ strategy settings
	cm.FAQPriorityEnabled = customAgent.Config.FAQPriorityEnabled
	cm.FAQDirectAnswerThreshold = customAgent.Config.FAQDirectAnswerThreshold
	cm.FAQScoreBoost = customAgent.Config.FAQScoreBoost
	if cm.FAQPriorityEnabled {
		logger.Infof(ctx, "FAQ priority enabled: threshold=%.2f, boost=%.2f",
			cm.FAQDirectAnswerThreshold, cm.FAQScoreBoost)
	}

	// Data-analysis pipeline stage (opt-in, default off).
	cm.DataAnalysisEnabled = customAgent.Config.DataAnalysisEnabled
	if cm.DataAnalysisEnabled {
		logger.Infof(ctx, "Data analysis pipeline stage enabled by custom agent")
	}

	if len(customAgent.Config.IntentPrompts) > 0 {
		cm.IntentPromptOverrides = customAgent.Config.IntentPrompts
		logger.Infof(ctx, "Using custom agent's intent_prompts (%d overrides)", len(cm.IntentPromptOverrides))
	}
}
