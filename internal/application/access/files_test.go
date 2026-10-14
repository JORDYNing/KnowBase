package access

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type fileCatalog struct{ resource *types.StoredResource }

func (c fileCatalog) ResolvePath(_ context.Context, ref string) (string, *types.StoredResource, error) {
	if c.resource == nil {
		return ref, nil, nil
	}
	return c.resource.PhysicalPath, c.resource, nil
}

type fileBinding struct {
	allowed bool
	err     error
	kb      string
	tenant  uint64
}

func (b *fileBinding) IsReferencedByKnowledgeBase(_ context.Context, tenant uint64, kb, _ string) (bool, error) {
	b.kb, b.tenant = kb, tenant
	return b.allowed, b.err
}

type fileMessages struct {
	message *types.Message
	tenant  uint64
}

func (m *fileMessages) GetMessage(ctx context.Context, _, _ string) (*types.Message, error) {
	m.tenant = types.MustTenantIDFromContext(ctx)
	return m.message, nil
}

func TestKBFilesRequireExactGrantAndBinding(t *testing.T) {
	base := callerContext()
	grant := &KBAccess{
		Caller:            types.CallerFromContext(base),
		KnowledgeBase:     &types.KnowledgeBase{ID: "kb", TenantID: 2},
		EffectiveTenantID: 2,
		Permission:        types.KBPermissionRead,
	}
	ctx := grant.Context(base)
	bindings := &fileBinding{allowed: true}
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	catalog := fileCatalog{
		&types.StoredResource{TenantID: 2, PhysicalPath: "local://2/exports/a.png", OriginalName: "a.png"},
	}
	file, err := ResolveKBFile(ctx, grant, "kb", ref, catalog, bindings)
	require.NoError(t, err)
	require.Equal(t, uint64(2), file.OwnerTenantID)
	require.Equal(t, "kb", bindings.kb)
	require.Equal(t, uint64(2), bindings.tenant)
	bindings.allowed = false
	_, err = ResolveKBFile(ctx, grant, "kb", ref, catalog, bindings)
	require.ErrorIs(t, err, ErrForbidden, "a same-tenant file still needs an exact KB binding")
	bindings.allowed = true
	_, err = ResolveKBFile(ctx, grant, "another", ref, catalog, bindings)
	require.ErrorIs(t, err, ErrForbidden)
	_, err = ResolveKBFile(ctx, nil, "kb", ref, catalog, bindings)
	require.ErrorIs(t, err, ErrForbidden, "execution tenant alone is not a grant")
	bindings.err = errors.New("database unavailable")
	_, err = ResolveKBFile(ctx, grant, "kb", ref, catalog, bindings)
	require.ErrorIs(t, err, bindings.err)
}

func TestMessageFilesRequireCallerTenantAndExactReference(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	messages := &fileMessages{message: &types.Message{AgentTenantID: 1, Role: "assistant", Content: ref}}
	catalog := fileCatalog{&types.StoredResource{TenantID: 1, PhysicalPath: "local://1/exports/a.png"}}
	_, err := ResolveMessageFile(callerContext(), "session", "message", ref, messages, catalog)
	require.NoError(t, err)
	catalog.resource.TenantID = 2
	catalog.resource.PhysicalPath = "local://2/exports/a.png"
	_, err = ResolveMessageFile(callerContext(), "session", "message", ref, messages, catalog)
	require.ErrorIs(t, err, ErrForbidden)
	catalog.resource.TenantID = 1
	messages.message.Content = ""
	_, err = ResolveMessageFile(callerContext(), "session", "message", ref, messages, catalog)
	require.ErrorIs(t, err, ErrForbidden)
}

func TestMessageArtifactsKeepSessionOwnershipSeparateFromAgentOutput(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	message := &types.Message{
		AgentID:       "agent",
		AgentTenantID: 2,
		Artifacts:     types.MessageArtifacts{{URL: ref, FileName: "report.pdf"}},
	}
	catalog := fileCatalog{&types.StoredResource{TenantID: 1, PhysicalPath: "local://1/exports/report.pdf"}}
	file, err := ResolveMessageArtifact(callerContext(), message, 0, catalog)
	require.NoError(t, err)
	require.Equal(t, uint64(1), file.OwnerTenantID)
	catalog.resource.TenantID = 2
	catalog.resource.PhysicalPath = "local://2/exports/report.pdf"
	_, err = ResolveMessageArtifact(callerContext(), message, 0, catalog)
	require.ErrorIs(t, err, ErrForbidden)
}

type messageFileKBs struct{ kb *types.KnowledgeBase }

func (k messageFileKBs) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return []*types.KnowledgeBase{k.kb}, nil
}

// This fixture represents a catalog claim made by the artifact collector.
type artifactFileCatalog struct{ fileCatalog }

func (artifactFileCatalog) GetMessageFileBindings(
	_ context.Context,
	_ uint64,
	_, messageID string,
) (*types.MessageFileBindings, error) {
	return &types.MessageFileBindings{MessageArtifact: messageID == "message"}, nil
}

// Deleting a generated file does not rewrite the answer that produced it, so
// its handle stays in message.Content. The message-scoped file proxy must not
// treat that leftover text as permission to keep serving the file — including
// when the bytes survived because a knowledge entry still holds them, and
// including for a shared-agent visitor who never had delete rights.
func TestDeletedArtifactIsNotServedFromTheAnswerText(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	deletedAt := time.Now()
	messages := &fileMessages{message: &types.Message{
		ID: "message", AgentID: "agent", AgentTenantID: 1, Role: "assistant",
		Content:   "![report](" + ref + ")",
		Artifacts: types.MessageArtifacts{{URL: ref, FileName: "report.pdf"}},
	}}
	catalog := artifactFileCatalog{
		fileCatalog{&types.StoredResource{TenantID: 1, PhysicalPath: "local://1/exports/report.pdf"}},
	}
	ctx := types.WithExecutionTenant(callerContext(), 2)

	_, err := ResolveMessageFile(ctx, "session", "message", ref, messages, catalog)
	require.NoError(t, err, "a live artifact is served as before")

	messages.message.Artifacts[0].DeletedAt = &deletedAt
	_, err = ResolveMessageFile(ctx, "session", "message", ref, messages, catalog)
	require.ErrorIs(t, err, ErrForbidden, "the handle left in the answer text must not re-authorize a deleted file")

	// The index-addressed artifact download agrees.
	_, err = ResolveMessageArtifact(ctx, messages.message, 0, catalog)
	require.ErrorIs(t, err, ErrNotFound)
}

// The same file can be attached twice at different positions. One of them
// still being live means the message really does still offer it.
func TestOneLiveCopyKeepsTheFileServable(t *testing.T) {
	const ref = "resource://AbCdEfGhIjKlMnOpQrStUv"
	deletedAt := time.Now()
	message := &types.Message{
		ID: "message", AgentTenantID: 1, Role: "assistant",
		Content: "![report](" + ref + ")",
		Artifacts: types.MessageArtifacts{
			{URL: ref, FileName: "report.pdf", DeletedAt: &deletedAt},
			{URL: ref, FileName: "report.pdf"},
		},
	}
	require.True(t, MessageReferencesFile(message, ref))

	message.Artifacts[1].DeletedAt = &deletedAt
	require.False(t, MessageReferencesFile(message, ref), "with every copy deleted the message no longer offers it")
}

// A reference the message never produced as an artifact keeps its old path:
// knowledge-base images and user attachments are authorized by the text.
func TestNonArtifactReferencesStillAuthorizeFromContent(t *testing.T) {
	const kbImage = "resource://ZzZzZzZzZzZzZzZzZzZzZz"
	deletedAt := time.Now()
	message := &types.Message{
		ID: "message", AgentTenantID: 1, Role: "assistant",
		Content:   "![kb](" + kbImage + ")",
		Artifacts: types.MessageArtifacts{{URL: "resource://AbCdEfGhIjKlMnOpQrStUv", DeletedAt: &deletedAt}},
	}
	require.True(t, MessageReferencesFile(message, kbImage))
}
