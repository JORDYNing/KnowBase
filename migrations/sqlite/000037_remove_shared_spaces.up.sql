DROP TABLE IF EXISTS tenant_disabled_shared_agents;
DROP TABLE IF EXISTS agent_shares;
DROP TABLE IF EXISTS kb_shares;
DROP TABLE IF EXISTS organization_join_requests;
DROP TABLE IF EXISTS organization_tenant_members;
DROP TABLE IF EXISTS organization_members_pre_plan3;
DROP TABLE IF EXISTS organization_members;
DROP TABLE IF EXISTS organizations;

UPDATE tenant_api_keys
SET capabilities = COALESCE((
    SELECT json_group_array(value)
    FROM json_each(tenant_api_keys.capabilities)
    WHERE value <> 'manage_spaces'
), '[]')
WHERE EXISTS (
    SELECT 1 FROM json_each(tenant_api_keys.capabilities)
    WHERE value = 'manage_spaces'
);
