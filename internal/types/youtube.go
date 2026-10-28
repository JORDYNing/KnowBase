package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// YouTubeTranscriptConfig is workspace-owned; proxy credentials never leave the configuration endpoint.
type YouTubeTranscriptConfig struct {
	Enabled         bool   `json:"enabled"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	ProxyURL        string `json:"proxy_url"`
}

func (c YouTubeTranscriptConfig) Value() (driver.Value, error) { return json.Marshal(c) }
func (c *YouTubeTranscriptConfig) Scan(value interface{}) error {
	if value == nil {
		*c = YouTubeTranscriptConfig{}
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	default:
		return fmt.Errorf("invalid YouTube config storage type %T", value)
	}
}
func YouTubeTranscriptConfigForResponse(c *YouTubeTranscriptConfig) *YouTubeTranscriptConfig {
	out := &YouTubeTranscriptConfig{}
	if c == nil {
		return out
	}
	*out = *c
	if u, err := url.Parse(out.ProxyURL); err == nil && u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "********")
		out.ProxyURL = u.String()
	}
	return out
}

// MergeYouTubeProxy keeps an omitted/redacted credential unchanged. Clear requires an explicit flag.
func MergeYouTubeProxy(incoming, previous string, clear bool) (string, error) {
	if clear {
		return "", nil
	}
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return previous, nil
	}
	u, err := url.Parse(incoming)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("代理地址须为 HTTP、HTTPS 或 SOCKS5 地址，不能包含路径、查询参数或片段")
	}
	if u.User != nil {
		if p, ok := u.User.Password(); ok && p == "********" {
			old, e := url.Parse(previous)
			if e != nil || old.User == nil || old.Host != u.Host || old.Scheme != u.Scheme || old.User.Username() != u.User.Username() {
				return "", fmt.Errorf("变更代理后请重新输入密码")
			}
			u.User = old.User
		}
	}
	return u.String(), nil
}

type YouTubeImportResult struct {
	VideoID           string `json:"video_id"`
	Title             string `json:"title"`
	Channel           string `json:"channel"`
	PublishedAt       string `json:"published_at"`
	Summary           string `json:"summary"`
	SummaryStatus     string `json:"summary_status"`
	URL               string `json:"url"`
	Language          string `json:"language"`
	KnowledgeID       string `json:"knowledge_id"`
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
	DocumentURL       string `json:"document_url"`
	Status            string `json:"status"`
	AlreadyExists     bool   `json:"already_exists"`
}

// ResolveDocumentURL resolves a document path against configured public SPA origins.
// It rejects credentials and protocol-relative or non-HTTP links.
func ResolveDocumentURL(path string, origins ...string) string {
	ref, err := url.Parse(strings.TrimSpace(path))
	if err != nil || ref.User != nil || path == "" {
		return ""
	}
	if ref.IsAbs() {
		if (ref.Scheme == "http" || ref.Scheme == "https") && ref.Hostname() != "" {
			return ref.String()
		}
		return ""
	}
	if ref.Host != "" || !strings.HasPrefix(ref.Path, "/") {
		return ""
	}
	for _, candidate := range origins {
		base, err := url.Parse(strings.TrimSpace(candidate))
		if err == nil && base.User == nil && base.Hostname() != "" && (base.Scheme == "http" || base.Scheme == "https") {
			return base.ResolveReference(ref).String()
		}
	}
	return ""
}
