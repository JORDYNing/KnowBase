package im

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type mentionTestAdapter struct {
	lifecycleTestAdapter
	mentioned bool
	err       error
	replies   int
}

func (a *mentionTestAdapter) IsBotMentioned(context.Context, *IncomingMessage) (bool, error) {
	return a.mentioned, a.err
}
func (a *mentionTestAdapter) SendReply(context.Context, *IncomingMessage, *ReplyMessage) error {
	a.replies++
	return nil
}

func TestSmartGroupMentionGate(t *testing.T) {
	for _, tc := range []struct {
		name              string
		platform          Platform
		chat              ChatType
		id, mode          string
		mentioned, ignore bool
	}{
		{"builtin unmentioned", PlatformFeishu, ChatTypeGroup, types.BuiltinSmartReasoningID, "", false, true},
		{"builtin mentioned", PlatformFeishu, ChatTypeGroup, types.BuiltinSmartReasoningID, "", true, false},
		{"direct", PlatformFeishu, ChatTypeDirect, types.BuiltinSmartReasoningID, "", false, false},
		{"lark", PlatformLark, ChatTypeGroup, types.BuiltinSmartReasoningID, "", false, true},
		{"custom smart", PlatformFeishu, ChatTypeGroup, "custom", types.AgentModeSmartReasoning, false, true},
		{"quick answer", PlatformFeishu, ChatTypeGroup, "custom", types.AgentModeQuickAnswer, false, false},
		{"other platform", PlatformWeCom, ChatTypeGroup, types.BuiltinSmartReasoningID, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{agentService: &stubIMAgentService{agent: &types.CustomAgent{ID: tc.id, Config: types.CustomAgentConfig{AgentMode: tc.mode}}}}
			got, err := s.shouldIgnoreUnmentionedGroupMessage(context.Background(), &IncomingMessage{Platform: tc.platform, ChatType: tc.chat}, &IMChannel{AgentID: tc.id}, &mentionTestAdapter{mentioned: tc.mentioned})
			if err != nil || got != tc.ignore {
				t.Fatalf("ignore=%v err=%v", got, err)
			}
		})
	}
}

func TestUnmentionedGroupNeverReachesReplyPipeline(t *testing.T) {
	for _, content := range []string{"hello", "", "/help", "/stop"} {
		for _, checkErr := range []error{nil, errors.New("identity unavailable")} {
			a := &mentionTestAdapter{err: checkErr}
			ch := &IMChannel{ID: "test", AgentID: types.BuiltinSmartReasoningID}
			s := &Service{channels: map[string]*channelState{"test": {Adapter: a, Channel: ch}}}
			// No command registry, rate limiter, or QA services: reaching them would panic.
			if err := s.HandleMessage(context.Background(), &IncomingMessage{Platform: PlatformFeishu, ChatType: ChatTypeGroup, Content: content}, "test"); err != nil {
				t.Fatal(err)
			}
			if a.replies != 0 {
				t.Fatalf("sent %d replies", a.replies)
			}
		}
	}
}

type mentionFileAdapter struct {
	mentionTestAdapter
	downloadErr error
	repliesCh   chan string
}

func (a *mentionFileAdapter) DownloadFile(context.Context, *IncomingMessage) (io.ReadCloser, string, error) {
	if a.downloadErr != nil {
		return nil, "", a.downloadErr
	}
	return io.NopCloser(strings.NewReader("group attachment")), "test.md", nil
}
func (a *mentionFileAdapter) SendReply(_ context.Context, _ *IncomingMessage, reply *ReplyMessage) error {
	a.repliesCh <- reply.Content
	return nil
}

type mentionTenantService struct{ interfaces.TenantService }

func (*mentionTenantService) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 42}, nil
}

type mentionKnowledgeService struct {
	interfaces.KnowledgeService
	saved   chan string
	release chan struct{}
	err     error
}

func (s *mentionKnowledgeService) CreateKnowledgeFromFile(_ context.Context, kbID string, file *multipart.FileHeader, _ map[string]string, _ *bool, _ string, _ []string, _ string, _ *types.KnowledgeProcessOverrides) (*types.Knowledge, error) {
	s.saved <- kbID + ":" + file.Filename
	<-s.release
	return &types.Knowledge{ID: "saved"}, s.err
}
func TestFileUploadConfirmationRegardlessOfMention(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		mentioned            bool
		kb                   string
		downloadErr, saveErr error
		want                 string
	}{
		{"without mention", false, "kb", nil, nil, "文件已入库"},
		{"with mention", true, "kb", nil, nil, "文件已入库"},
		{"no knowledge base", false, "", nil, nil, "未配置知识库，文件未入库。"},
		{"download failure", false, "kb", errors.New("download failed"), nil, "文件读取失败，未入库，请重试。"},
		{"save failure", false, "kb", nil, errors.New("save failed"), "文件入库失败，请重试。"},
		{"unrelated duplicate error", false, "kb", nil, errors.New("duplicate key"), "文件入库失败，请重试。"},
		{"duplicate", false, "kb", nil, types.NewDuplicateFileError(&types.Knowledge{ID: "existing", FileName: "test.md"}), "文件已在知识库中"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &mentionFileAdapter{mentionTestAdapter: mentionTestAdapter{mentioned: tc.mentioned}, downloadErr: tc.downloadErr, repliesCh: make(chan string, 2)}
			ch := &IMChannel{ID: "test", TenantID: 42, AgentID: types.BuiltinSmartReasoningID, KnowledgeBaseID: tc.kb}
			saved := make(chan string, 1)
			release := make(chan struct{})
			s := &Service{channels: map[string]*channelState{"test": {Adapter: a, Channel: ch}}, tenantService: &mentionTenantService{}, knowledgeService: &mentionKnowledgeService{saved: saved, release: release, err: tc.saveErr}}
			err := s.HandleMessage(context.Background(), &IncomingMessage{Platform: PlatformFeishu, ChatType: ChatTypeGroup, MessageType: MessageTypeFile, FileKey: "file", FileName: "test.md"}, "test")
			if err != nil {
				t.Fatal(err)
			}
			if tc.kb != "" && tc.downloadErr == nil {
				select {
				case got := <-saved:
					if got != "kb:test.md" {
						t.Errorf("saved %q", got)
					}
				case <-time.After(3 * time.Second):
					close(release)
					t.Fatal("attachment save not attempted")
				}
				select {
				case got := <-a.repliesCh:
					t.Errorf("premature confirmation: %s", got)
				default:
				}
			}
			close(release)
			select {
			case got := <-a.repliesCh:
				if got != tc.want {
					t.Fatalf("reply=%q want=%q", got, tc.want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("missing file confirmation")
			}
			select {
			case got := <-a.repliesCh:
				t.Fatalf("duplicate confirmation: %s", got)
			default:
			}
		})
	}
}
