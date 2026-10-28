package types

import "context"

type youtubeExplicitLanguageKey struct{}

// WithYouTubeExplicitLanguageRequired is set by the trusted IM message parser
// when the user specifies a language that needs model interpretation. It does
// not grant any permissions; it only prevents a missing argument from silently
// selecting the default language.
func WithYouTubeExplicitLanguageRequired(ctx context.Context) context.Context {
	return context.WithValue(ctx, youtubeExplicitLanguageKey{}, true)
}

func YouTubeExplicitLanguageRequired(ctx context.Context) bool {
	required, _ := ctx.Value(youtubeExplicitLanguageKey{}).(bool)
	return required
}

// WithYouTubeImportProgress reports saved transcript metadata before waiting for its summary.
func WithYouTubeImportProgress(ctx context.Context, notify func(*YouTubeImportResult)) context.Context {
	return context.WithValue(ctx, youtubeImportProgressKey{}, notify)
}

type youtubeImportProgressKey struct{}

func NotifyYouTubeImportProgress(ctx context.Context, result *YouTubeImportResult) {
	if notify, ok := ctx.Value(youtubeImportProgressKey{}).(func(*YouTubeImportResult)); ok && notify != nil {
		snapshot := *result
		notify(&snapshot)
	}
}
