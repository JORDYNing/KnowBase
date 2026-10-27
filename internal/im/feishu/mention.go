package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/Tencent/WeKnora/internal/im"
)

// IsBotMentioned matches event mention IDs against this application's bot.
func (a *Adapter) IsBotMentioned(ctx context.Context, msg *im.IncomingMessage) (bool, error) {
	if len(msg.MentionedUserIDs) == 0 {
		return false, nil
	}
	a.botMu.Lock()
	defer a.botMu.Unlock()
	if a.botOpenID == "" {
		token, err := a.getTenantAccessToken(ctx)
		if err != nil {
			return false, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.api("/open-apis/bot/v3/info"), nil)
		if err != nil {
			return false, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(req)
		if err != nil {
			return false, err
		}
		defer resp.Body.Close()
		var result struct {
			Code int `json:"code"`
			Bot  struct {
				OpenID string `json:"open_id"`
			} `json:"bot"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return false, err
		}
		if resp.StatusCode != http.StatusOK || result.Code != 0 || result.Bot.OpenID == "" {
			return false, fmt.Errorf("get bot identity: status=%d code=%d", resp.StatusCode, result.Code)
		}
		a.botOpenID = result.Bot.OpenID
	}
	return slices.Contains(msg.MentionedUserIDs, a.botOpenID), nil
}
