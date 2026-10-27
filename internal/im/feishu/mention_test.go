package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/im"
	"github.com/gin-gonic/gin"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestMentionIDsBothTransports(t *testing.T) {
	for _, msgType := range []string{"text", "post", "image", "file"} {
		contents := map[string]string{"text": `{"text":"@_user_1 hello"}`, "post": `{"content":[[{"tag":"at","user_id":"ou_bot"},{"tag":"text","text":"hello"}]]}`, "image": `{"image_key":"img_1"}`, "file": `{"file_key":"file_1","file_name":"test.md"}`}
		for _, mentions := range []string{`[]`, `[{"id":{"open_id":"ou_other"}}]`, `[{"id":{"open_id":"ou_other"}},{"id":{"open_id":"ou_bot"}}]`} {
			content, _ := json.Marshal(contents[msgType])
			body := fmt.Sprintf(`{"header":{"event_type":"im.message.receive_v1"},"event":{"message":{"message_type":%q,"chat_type":"group","content":%s,"mentions":%s}}}`, msgType, content, mentions)
			var event larkim.P2MessageReceiveV1
			if err := json.Unmarshal([]byte(body), &event); err != nil {
				t.Fatal(err)
			}
			ws := convertEvent(RegionFeishu, &event)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
			hook, err := (&Adapter{region: RegionFeishu}).ParseCallback(c)
			if err != nil || ws == nil || hook == nil {
				t.Fatalf("%s: parse failure %v", msgType, err)
			}
			var expected []string
			if strings.Contains(mentions, "ou_other") {
				expected = append(expected, "ou_other")
			}
			if strings.Contains(mentions, "ou_bot") {
				expected = append(expected, "ou_bot")
			}
			if !reflect.DeepEqual(ws.MentionedUserIDs, expected) || !reflect.DeepEqual(hook.MentionedUserIDs, expected) {
				t.Fatalf("%s: websocket=%v webhook=%v want=%v", msgType, ws.MentionedUserIDs, hook.MentionedUserIDs, expected)
			}
		}
	}
}

func TestBotMentionIdentityAndCache(t *testing.T) {
	useTestHTTPClient(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/open-apis/bot/v3/info" || r.Header.Get("Authorization") != "Bearer token" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"code":0,"bot":{"open_id":"ou_bot"}}`)
	}))
	defer server.Close()
	a := &Adapter{apiBaseURL: server.URL, tokenCache: "token", tokenExpAt: time.Now().Add(time.Hour)}
	for _, tc := range []struct {
		ids  []string
		want bool
	}{{nil, false}, {[]string{"ou_other"}, false}, {[]string{"all"}, false}, {[]string{"ou_other", "ou_bot"}, true}} {
		got, err := a.IsBotMentioned(context.Background(), &im.IncomingMessage{MentionedUserIDs: tc.ids})
		if err != nil || got != tc.want {
			t.Fatalf("ids=%v got=%v err=%v", tc.ids, got, err)
		}
	}
	if requests != 1 {
		t.Fatalf("identity requests=%d want=1", requests)
	}
}

func TestBotMentionIdentityFailure(t *testing.T) {
	useTestHTTPClient(t)
	for _, body := range []string{`{"code":1}`, `{"code":0,"bot":{}}`, `invalid`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		a := &Adapter{apiBaseURL: server.URL, tokenCache: "token", tokenExpAt: time.Now().Add(time.Hour)}
		mentioned, err := a.IsBotMentioned(context.Background(), &im.IncomingMessage{MentionedUserIDs: []string{"ou_bot"}})
		server.Close()
		if mentioned || err == nil {
			t.Fatalf("body=%s mentioned=%v err=%v", body, mentioned, err)
		}
	}
}
