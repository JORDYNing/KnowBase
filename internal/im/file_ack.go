package im

import (
	"context"

	"github.com/Tencent/WeKnora/internal/logger"
)

func (s *Service) saveIMFileAndReply(ctx context.Context, msg *IncomingMessage, channel *IMChannel, adapter Adapter) {
	content := "未配置知识库，文件未入库。"
	if channel.KnowledgeBaseID != "" {
		_, _, downloaded, err := s.prepareIMAttachments(ctx, msg, adapter)
		if err != nil {
			logger.Warnf(ctx, "[IM] File download failed: %v", err)
			content = "文件读取失败，未入库，请重试。"
		} else {
			created, err := s.processDownloadedFileToKnowledgeBase(ctx, channel, downloaded)
			if err != nil {
				content = "文件入库失败，请重试。"
			} else if created {
				content = "文件已入库"
			} else {
				content = "文件已在知识库中"
			}
		}
	}
	if err := adapter.SendReply(ctx, msg, &ReplyMessage{Content: content, IsFinal: true}); err != nil {
		logger.Warnf(ctx, "[IM] File confirmation reply failed: %v", err)
	}
}
