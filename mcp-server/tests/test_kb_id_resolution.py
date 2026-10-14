import pytest

from weknora_mcp_server import WeKnoraClient, _normalize_kb_entries


def test_normalize_current_workspace_rows():
    assert _normalize_kb_entries({"data": [{"id": "kb-1", "name": "Team docs"}]}) == [{"id": "kb-1", "name": "Team docs"}]
    assert _normalize_kb_entries({"data": {"items": [{"id": "kb-2"}]}}) == [{"id": "kb-2"}]


def test_resolve_kb_name_only_queries_current_workspace(monkeypatch):
    client = WeKnoraClient("http://example.test/api/v1", "")
    calls = []
    def request(method, path, **kwargs):
        calls.append(path)
        assert path == "/knowledge-bases"
        return {"data": [{"id": "kb-1", "name": "Team docs"}]}
    monkeypatch.setattr(client, "_request", request)
    assert client.resolve_kb_id("TEAM DOCS") == "kb-1"
    assert calls == ["/knowledge-bases"]


def test_unknown_kb_does_not_fall_back_to_shared_workspace(monkeypatch):
    client = WeKnoraClient("http://example.test/api/v1", "")
    def request(method, path, **kwargs):
        assert path == "/knowledge-bases"
        return {"data": []}
    monkeypatch.setattr(client, "_request", request)
    with pytest.raises(ValueError, match="not found"):
        client.resolve_kb_id("missing")
