# 平台管理与系统管理员

系统管理员负责整个 Gamelib 部署的全局设置、任务队列、平台 API Key 和跨空间审计。空间 Owner 负责单个工作空间，两类身份分别授予。空间角色说明见[租户、用户与认证授权](01-tenant-auth.md)。

两类身份的管理范围如下：

| | 空间 Owner | 系统管理员 |
| --- | --- | --- |
| 作用范围 | 单个工作空间 | 整个部署 |
| 授予方式 | 空间 10000 通过指定飞书邮箱一次性初始化，后续由原 Owner 转让 | 由现有系统管理员授予，首个管理员通过环境变量引导 |
| 管理内容 | 空间成员、模型、知识库、集成、空间审计 | 全局系统设置、任务队列、平台 API Key、跨空间审计、重置用户密码 |
| 是否自动叠加 | — | **不会**：在某空间是 Owner 不代表是系统管理员，反之亦然 |

跨空间数据访问由 `CanAccessAllTenants` 单独控制。系统管理员身份不会自动授予其他空间的知识库内容访问权。

<Screenshot
  src="/screenshots/settings-system-admin.png"
  caption="平台控制台：系统设置、任务队列、平台 API Key 与系统审计日志"
  hint="以系统管理员身份打开「设置」，展示侧栏底部四个仅系统管理员可见的分区，正文可用系统设置页。" />

## 设置首个系统管理员 {#_1-第一个系统管理员怎么来}

新部署需要先设置首个系统管理员：

1. 先通过飞书认证创建账号；
2. 为 app 服务配置 `WEKNORA_BOOTSTRAP_SYSTEM_ADMIN_EMAIL=<该账号邮箱>`，重启；
3. 启动时仅在部署尚无系统管理员的情况下，将该邮箱对应的用户设为系统管理员。

引导流程只提升已存在的账号。账号尚不存在时会记录告警，后续重启再次检查；配置错误不阻断服务启动。部署已有系统管理员后，该变量不再授予权限。

后续可在界面中添加或撤销系统管理员。不能撤销自己，也不能撤销最后一位系统管理员。对应接口为 `POST /system/admin/promote` 和 `/revoke`；重复撤销非管理员返回 200，审计记录以 `changed=false` 标明未发生权限变更。

## 使用平台控制台 {#_2-控制台能做什么}

系统管理员可在「设置」侧栏查看以下管理分区：

| 分区 | 作用 | 接口 |
| --- | --- | --- |
| 系统设置 | 可修改的全局运行时开关（并发、SSRF 白名单、存储配额等），按设置项的生效规则应用，见下方设置表 | `GET/PUT/DELETE /system/admin/settings[/:key]` |
| 模型目录 | 查看目录模型及其来源，编辑、添加模型或用 JSON 批量修改（保存即生效），按版本历史恢复；详见[模型目录管理](06-models.md#系统管理员维护模型目录) | `/system/admin/model-catalog*` |
| 任务队列 | 查看 asynq 各队列实时积压、逐个任务的重试/归档/删除、批量清空归档任务；Lite 模式返回 `available=false` | `/system/admin/runtime/queues*` |
| 平台 API Key | 面向控制面自动化的 platform 作用域 Key，能力包括 `system_tenants_read/manage`、`system_settings_read/manage`、`system_runtime_read/manage`、`system_audit_read` | `/system/admin/api-keys` |
| 系统审计日志 | `tenant_id = 0` 的平台级事件（改设置、提升/撤销管理员、队列操作等）。空间级审计接口按 tenant 过滤，看不到这些行 | `GET /system/admin/audit-log` |

系统管理员还可执行以下操作：

- **重置用户密码**（`POST /system/admin/users/reset-password`）：替换目标用户的本地密码并吊销其全部会话。**不能给自己重置**；本地密码不会开启登录能力，人类登录仍仅允许飞书；
- **批量套用默认存储配额**（`POST /system/admin/tenants/apply-default-storage-quota`）：把当前的默认配额写到所有已存在的空间上。该操作更新已有空间的配额数据。

### 创建用户

自 v0.8.2 起，系统管理员可在「设置 → 系统设置 → 账户与访问」中点击「创建用户」开通本地账号：填写用户名（2–50 字符）和邮箱，保持「自动生成随机密码」开启，或关闭后手动填写符合密码策略的密码。该兼容管理接口仅创建无空间身份，不创建个人空间，也不提供密码登录能力。用户仍需经飞书认证绑定身份，再按团队申请流程进入空间 10000。

<Screenshot
  src="/screenshots/system-admin-create-user.png"
  caption="系统管理员创建用户：填写账号信息与一次性密码展示"
  hint="「设置 → 系统设置 → 账户与访问」下打开「创建用户」对话框，展示用户名、邮箱与「自动生成随机密码」开关；可另附创建成功后显示一次性密码和「复制账号信息」按钮的结果页。" />

兼容响应中的随机密码仅展示一次，但不能用于本项目登录；请引导用户使用飞书。每次创建都会写入系统审计日志（`system.user_created`）。重复身份返回已有用户，不改密码；邮箱和用户名分别指向不同用户时拒绝创建。接口见[系统 API](../04-api/02-api-system.md)。

创建用户与首个管理员引导是不同操作：bootstrap 仍只提升已存在用户，不负责创建账号。

## 运行时设置参考 {#_3-运行时可改的系统设置}

支持的运行时设置可在控制台中修改。`auth.registration_mode=invite_only`、`auth.default_tenant_mode=tenantless`、`tenant.self_service_creation_enabled=false` 为只读兼容值，不能重新开启注册、个人空间或自助创建。数据库中的设置值优先于环境变量，多数立即生效；影响新资源或需重建运行时的设置以表中说明为准。

| 键 | 类型 | 默认 | 生效时机 |
| --- | --- | --- | --- |
| `tenant.default_storage_quota_gb` | int | `10` | **仅新建空间时读取**，不回写已有空间 |
| `tenant.auto_create_api_key` | bool | `false` | 每次建空间时读取 |
| `ssrf.whitelist` | 字符串列表 | 空 | 立即（`SSRF_WHITELIST_EXTRA` 仍只由部署方维护，不在此覆盖） |
| `asynq.core/postprocess/enrichment/maintenance/shared/wiki_concurrency` | int | 见[异步任务系统](../02-architecture/05-async-tasks.md) | 各 worker pool 重新装配 |
| `model.max_concurrency` | int | `32` | 立即 |

以下开关同样遵循数据库优先规则：

| 键 | 默认 | 作用 |
| --- | --- | --- |
| `auth.complex_password_enabled` | false | 仅影响保留的后台账号创建/密码重置，不能开启密码登录 |
| `tenant.auto_accept_invitation` | false | 邮箱邀请已注册用户时直接写入成员关系 |
| `sandbox.docker_enabled` | false | 开放 Docker 沙箱配置和实例创建 |

`tenant.auto_create_api_key` 默认关闭。平台管理创建空间时可使用此兼容开关，自动创建 full-access Key 并返回明文；飞书新账号不会触发创建空间。

::: tip 重置运行时设置
控制台保存设置后，修改对应环境变量不会覆盖数据库值。排查配置未生效时，先检查运行时设置；重置（`DELETE /system/admin/settings/:key`）会删除数据库覆盖值，恢复使用环境变量或内置默认值。
:::

## 相关文档 {#相关}

- 空间内的四级角色与 API Key：[租户、用户与认证授权](01-tenant-auth.md)
- 队列拓扑与 worker pool：[异步任务系统](../02-architecture/05-async-tasks.md)
- 审计日志与追踪：[可观测性与审计](16-observability.md)
- 接口清单：[系统与平台管理 API](../04-api/02-api-system.md)

## 实现参考

- `cmd/server/bootstrap.go`：首个系统管理员引导。
- `frontend/src/config/settingsAccess.ts`：平台设置分区。
- `internal/application/service/system_setting.go`：运行时设置注册表。
