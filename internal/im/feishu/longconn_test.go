package feishu

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Tencent/WeKnora/internal/im"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestConvertPostEventPreservesEmbeddedImage(t *testing.T) {
	content := `{"title":"","content":[[{"tag":"at","user_id":"ou_bot"},{"tag":"text","text":"请描述这张图"},{"tag":"img","image_key":"img_v3_abc"}]]}`
	msg := &larkim.EventMessage{Content: &content}

	got := convertPostEvent(RegionFeishu, msg, "ou_user", "oc_group", im.ChatTypeGroup, "om_message")
	if got == nil {
		t.Fatal("convertPostEvent() returned nil")
	}
	if got.MessageType != im.MessageTypeImage {
		t.Fatalf("MessageType = %q, want %q", got.MessageType, im.MessageTypeImage)
	}
	if got.Content != "请描述这张图" {
		t.Fatalf("Content = %q, want %q", got.Content, "请描述这张图")
	}
	if got.FileKey != "img_v3_abc" {
		t.Fatalf("FileKey = %q, want %q", got.FileKey, "img_v3_abc")
	}
	if got.FileName != "img_v3_abc.png" {
		t.Fatalf("FileName = %q, want %q", got.FileName, "img_v3_abc.png")
	}
}

func TestConvertPostEventPreservesImageWithoutText(t *testing.T) {
	content := `{"title":"","content":[[{"tag":"at","user_id":"ou_bot"},{"tag":"img","image_key":"img_v3_only"}]]}`
	msg := &larkim.EventMessage{Content: &content}

	got := convertPostEvent(RegionFeishu, msg, "ou_user", "oc_group", im.ChatTypeGroup, "om_message")
	if got == nil {
		t.Fatal("convertPostEvent() returned nil")
	}
	if got.MessageType != im.MessageTypeImage {
		t.Fatalf("MessageType = %q, want %q", got.MessageType, im.MessageTypeImage)
	}
	if got.Content != "" {
		t.Fatalf("Content = %q, want empty", got.Content)
	}
	if got.FileKey != "img_v3_only" {
		t.Fatalf("FileKey = %q, want %q", got.FileKey, "img_v3_only")
	}
}

func TestConvertPostEventKeepsTextOnlyPostsAsText(t *testing.T) {
	content := `{"title":"Status","content":[[{"tag":"text","text":"all systems go"}]]}`
	msg := &larkim.EventMessage{Content: &content}

	got := convertPostEvent(RegionFeishu, msg, "ou_user", "oc_group", im.ChatTypeGroup, "om_message")
	if got == nil {
		t.Fatal("convertPostEvent() returned nil")
	}
	if got.MessageType != im.MessageTypeText {
		t.Fatalf("MessageType = %q, want %q", got.MessageType, im.MessageTypeText)
	}
	if got.Content != "Status\nall systems go" {
		t.Fatalf("Content = %q, want %q", got.Content, "Status\\nall systems go")
	}
	if got.FileKey != "" {
		t.Fatalf("FileKey = %q, want empty", got.FileKey)
	}
}

func TestPostFileAttachments(t *testing.T) {
	payload := `{"title":"","content":[[{"tag":"text","text":"上传文件","style":[]}]],"content_v2":[[{"tag":"text","text":"上传文件","style":[]}]],"files":[{"file_key":"file_v3_x","file_name":"测试.md","is_folder":false}]}`
	for _, tc := range []struct {
		name, content, text, fileKey, fileName string
		messageType                            im.MessageType
	}{
		{"file with text", payload, "上传文件", "file_v3_x", "测试.md", im.MessageTypeFile},
		{"localized file", `{"zh_cn":` + payload + `}`, "上传文件", "file_v3_x", "测试.md", im.MessageTypeFile},
		{"file without text", `{"files":[{"file_key":"file_v3_x","file_name":"测试.md","is_folder":false}]}`, "", "file_v3_x", "测试.md", im.MessageTypeFile},
		{"first file", `{"files":[{"file_key":"file_v3_x","file_name":"测试.md"},{"file_key":"file_v3_y","file_name":"second.md"}]}`, "", "file_v3_x", "测试.md", im.MessageTypeFile},
		{"embedded image", `{"content":[[{"tag":"text","text":"图片"},{"tag":"img","image_key":"img_v3_x"}]]}`, "图片", "img_v3_x", "img_v3_x.png", im.MessageTypeImage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, transport := range []string{"websocket", "webhook"} {
				t.Run(transport, func(t *testing.T) {
					var got *im.IncomingMessage
					if transport == "websocket" {
						got = convertPostEvent(RegionFeishu, &larkim.EventMessage{Content: &tc.content}, "ou_user", "oc_group", im.ChatTypeGroup, "om_message")
					} else {
						encodedContent, err := json.Marshal(tc.content)
						if err != nil {
							t.Fatal(err)
						}
						body := `{"header":{"event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"ou_user"}},"message":{"message_type":"post","message_id":"om_message","root_id":"om_root","chat_type":"group","chat_id":"oc_group","content":` + string(encodedContent) + `}}}`
						ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
						ctx.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
						got, err = (&Adapter{region: RegionFeishu}).ParseCallback(ctx)
						if err != nil {
							t.Fatal(err)
						}
						if got != nil && got.ThreadID != "om_root" {
							t.Fatalf("ThreadID = %q", got.ThreadID)
						}
					}
					if got == nil {
						t.Fatal("post returned nil")
					}
					if got.MessageType != tc.messageType || got.Content != tc.text || got.FileKey != tc.fileKey || got.FileName != tc.fileName {
						t.Fatalf("unexpected attachment: %+v", got)
					}
					if got.MessageID != "om_message" || got.UserID != "ou_user" || got.ChatID != "oc_group" || got.ChatType != im.ChatTypeGroup || got.Platform != RegionFeishu.Platform {
						t.Fatalf("unexpected message metadata: %+v", got)
					}
				})
			}
		})
	}
}
