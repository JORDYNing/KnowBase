# 飞书登录与团队入驻 API

本项目仅允许飞书登录。密码注册、密码登录、邀请注册、OIDC、修改密码和桌面自动登录接口均关闭。机器认证保留原有 API Key、MCP、嵌入能力。

认证和审批流程、角色规则、请求字段及故障处理见[团队知识空间与飞书认证](../03-features/01-tenant-auth.md)。完整接口参数见生成的 Swagger 文档。

## 会话接口

| 接口 | 说明 |
| --- | --- |
| `GET /api/v1/auth/feishu/config` | 飞书登录可用性 |
| `GET /api/v1/auth/feishu/start` | 302 跳转飞书授权 |
| `GET /api/v1/auth/feishu/callback` | 完成认证并返回本地 JWT；首次登录自动申请加入 10000 |
| `GET /api/v1/auth/me` | 当前身份、有效空间、成员角色及能力；待审批时空间为空 |
| `POST /api/v1/auth/switch-tenant` | 成员资格校验成功后签发目标空间凭据 |
| `POST /api/v1/auth/refresh` | 刷新 JWT |
| `GET /api/v1/auth/validate` | 校验 JWT |
| `POST /api/v1/auth/logout` | 撤销会话 |

`/me/tenant-join-request` 查询与提交接口对无空间的有效飞书会话开放；其他空间资源仍要求有效成员身份。Owner/Admin 可在成员管理的加入申请页审批，批准时角色必填。
