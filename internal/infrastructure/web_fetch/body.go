package web_fetch

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

// Bound the decompressed download independently of the markup retained for
// extraction. Script/style payloads are not article content. Never treat a
// truncated document as a successful fetch, including in the chat pipeline.
func readPageBody(reader io.Reader, header string, contentLimit int64) ([]byte, string, error) {
	limited := &io.LimitedReader{R: reader, N: maxDownloadSize + 1}
	buffered := bufio.NewReader(limited)
	contentType, _, _ := mime.ParseMediaType(header)
	if contentType == "" {
		prefix, _ := buffered.Peek(512)
		contentType, _, _ = mime.ParseMediaType(http.DetectContentType(prefix))
	}
	var body []byte
	var err error
	if isHTMLContent(contentType) {
		body, err = compactHTML(buffered, contentLimit)
	} else {
		body, err = io.ReadAll(io.LimitReader(buffered, contentLimit+1))
	}
	if limited.N <= 0 || err == html.ErrBufferExceeded {
		return nil, contentType, newFetchError(ErrorBodyTooLarge, false,
			"page exceeds the %d byte download limit", maxDownloadSize)
	}
	if err != nil {
		return nil, contentType, newFetchError(ErrorRead, true, "read failed: %v", err)
	}
	if int64(len(body)) > contentLimit {
		return nil, contentType, newFetchError(ErrorBodyTooLarge, false,
			"page content exceeds the %d byte extraction limit", contentLimit)
	}
	return body, contentType, nil
}

func compactHTML(reader io.Reader, limit int64) ([]byte, error) {
	tokens := html.NewTokenizer(reader)
	tokens.SetMaxBuf(maxDownloadSize + 1)
	var output bytes.Buffer
	var discard string
	for {
		kind := tokens.Next()
		if kind == html.ErrorToken {
			if err := tokens.Err(); err != io.EOF {
				return nil, err
			}
			return output.Bytes(), nil
		}
		if kind == html.CommentToken {
			continue
		}
		if kind == html.StartTagToken || kind == html.EndTagToken || kind == html.SelfClosingTagToken {
			name, _ := tokens.TagName()
			tag := strings.ToLower(string(name))
			if discard != "" {
				if kind == html.EndTagToken && tag == discard {
					discard = ""
				} else {
					continue
				}
			} else if kind == html.StartTagToken && (tag == "script" || tag == "style") {
				discard = tag
			}
		} else if discard != "" {
			continue
		}
		// Retain empty script tags so SPA detection can still request rendering.
		raw := tokens.Raw()
		remaining := limit + 1 - int64(output.Len())
		if int64(len(raw)) > remaining {
			raw = raw[:remaining]
		}
		output.Write(raw)
		if int64(output.Len()) > limit {
			return output.Bytes(), nil
		}
	}
}
