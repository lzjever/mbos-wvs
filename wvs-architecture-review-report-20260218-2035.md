# WVS 架构复审结果报告

## 0. 基本信息
- 审查对象文档：`wvs-tech-docs.md`
- 任务书：`wvs-architecture-review-handover.md`
- 审查团队：外部架构评审
- 审查负责人：—
- 审查日期：2026-02-18
- 版本号：v1.0

## 1. 复审结论摘要
### 1.1 总体结论
- 结论：`条件通过` → **修订后：`通过`**（P0 全部修复，P1 全部修复，P2 全部修复或已接受）
- 结论说明：
  1. 整体架构设计清晰，五面模型分层合理，MVP/GA 边界划分明确
  2. API 契约、DDL、状态机三者一致，主流程可实现
  3. 初审发现 1 个 P0 问题、6 个 P1 问题、6 个 P2 问题
  4. **修订后：P0-01 已修复；全部 6 个 P1 已修复；P2 已修复 7 项（P2-01~P2-03, P2-05~P2-06），P2-04（hashtext 碰撞）为已接受的已知限制归入 GA**
  5. 文档规范性强，使用 MUST/SHOULD 术语，范围冻结与变更门禁机制有效
  6. 所有问题均已处置，文档可直接指导下一轮开发

### 1.2 MVP可落地结论
- `snapshot create -> set_current -> snapshot drop` 主流程可实现：`有条件`
- 关键阻塞项：
  1. **P0-01**：snapshot_drop 引用检测存在 API 层与 worker 层之间的竞态窗口，可导致删除被活跃任务引用的快照

### 1.3 MVP/GA边界结论
- 边界清晰且无重大冲突：`有条件`
- 冲突点：
  1. docker-compose.yml 包含 GA 组件（vmauth/vmalert/grafana/alertmanager），可能误导开发团队认为 MVP 必须交付这些组件
  2. wvs-index 工具为 GA 项但出现在 cmd/ 目录结构中，需明确标注

## 2. 问题清单（按严重级别）
> 分级：P0 阻断落地；P1 高风险；P2 优化项

| ID | 级别 | 领域 | 问题描述 | 证据（章节/接口/DDL） | 影响 | 建议修订 | 责任方 | 目标时间 |
|---|---|---|---|---|---|---|---|---|
| P0-01 | P0 | 一致性/并发 | snapshot_drop 引用检测与 API 创建任务之间存在竞态窗口：worker 持 advisory lock 执行引用检查通过后、executor 删除目录前，wvs-api 可并发插入引用同一 snapshot 的 set_current PENDING 任务（API 层不持 advisory lock），导致目录被删除后新任务 clone 失败 | docs/06 状态机 + docs/04 API snapshot_drop + set_current | 数据不一致：已删除的 snapshot 目录无法被后续 set_current 克隆，任务必然失败 | 方案 A（推荐）：snapshot_drop 在 worker 事务内（持 advisory lock 时）**再次**检查引用并写入 deleted_at（标记软删），然后 executor 删除目录；方案 B：API 层创建 set_current 任务时在同一事务内检查 snapshot 的 deleted_at 状态 | 架构组 | MVP 编码前 | **已修正** ✅ 采用方案 A：反转执行顺序为"先在持锁事务内标记 deleted_at、后异步删除目录"，同步修改了 docs/01 时序、docs/06 关键业务规则 |
| P1-01 | P1 | 接口契约 | set_current 成功后文档仅要求更新 `workspaces.current_snapshot_id`，未提及同步更新 `workspaces.current_path`；但 DDL 中 current_path 为 NOT NULL 字段，GET /current API 返回 current_path | docs/01 Set Current 时序 + docs/05 DDL workspaces 表 + docs/04 GET /current | current_path 返回过时值，agent 可能读到错误路径 | 在 docs/01 Set Current 第 4 步明确写入："worker 更新 `workspaces.current_snapshot_id` **和 `current_path`**" | 开发组 | MVP 阶段 | **已修正** ✅ |
| P1-02 | P1 | 功能闭环 | CANCELED 状态存在但无触发路径：状态机定义了 PENDING→CANCELED 和 RUNNING→CANCELED 转换，但 API 未提供任何 cancel 端点，disable 操作又要求无活跃任务 | docs/06 状态机 mermaid 图 + docs/04 API 端点列表 | 死代码风险：CANCELED 状态不可达，增加理解和维护成本；或暗示缺失 cancel 功能 | 方案 A（推荐）：MVP 移除 CANCELED 状态，DDL CHECK 约束移除该值；方案 B：增加 `POST /v1/tasks/{task_id}:cancel` 端点 | 架构组 | MVP 阶段 | **已修正** ✅ 采用方案 B：增加 `POST /v1/tasks/{task_id}:cancel` 端点 + DDL 增加 `cancel_requested` 字段 |
| P1-03 | P1 | 执行面幂等 | 任务重试时 executor 层缺乏文件系统级幂等保障：若首次执行 clone 成功但 worker 未能更新 DB（如网络闪断），重试会再次 clone 到相同/不同目录 | docs/06 重试机制 + docs/13 开发指南 executor 流程 | snapshot_create 重试可能产生孤儿快照目录；set_current 重试可能产生孤儿 live 目录 | 在 executor 执行前增加"目标目录已存在"检查：snapshot_create 若目标目录已存在且 snapshot.json 完整则视为已完成；set_current 若 current symlink 已指向目标则视为已完成 | 开发组 | MVP 阶段 | **已修正** ✅ 三个 executor 任务均增加了 MUST 级幂等前置检查 |
| P1-04 | P1 | 部署/Demo | docker-compose.yml 缺少 migration job：文档明确要求"数据库迁移只允许由独立 migration job 执行；业务进程启动时禁止自动迁移"，但 docker-compose 中无 migration 服务 | docs/00 MVP 固化决策第 4 条 + docs/12 docker-compose.yml | demo 无法一键启动：wvs-api/wvs-worker 连接数据库时 schema 不存在 | 在 docker-compose.yml 增加 `wvs-migrate` 服务，使用 golang-migrate 执行迁移，wvs-api/wvs-worker 依赖该服务 `service_completed_successfully` | 开发组 | MVP 阶段 | **已修正** ✅ 增加 wvs-migrate 服务，wvs-api/wvs-worker 依赖链已更新 |
| P1-05 | P1 | 接口契约 | disable（DELETE /v1/workspaces/{wsid}）后资源可见性和可操作性规则未定义：禁用后的 workspace 能否 list snapshots？能否查询 tasks？能否再次 disable？ | docs/04 API DELETE workspace + 移交任务书 §7.5 | 开发团队需自行决定语义，可能导致实现不一致 | 在 docs/04 中补充禁用后行为规则：(1) GET workspace/snapshot/task/current 仍可查询（只读）；(2) 所有写操作返回 `WVS_GONE 410`；(3) 重复 disable 幂等返回 200 | 架构组 | MVP 阶段 | **已修正** ✅ 在 DELETE workspace 端点下补充了幂等行为和禁用后行为规则 |
| P1-06 | P1 | 协议 | quiesce 超时时间未明确定义：EXECUTOR_TASK_TIMEOUT=300s 是整个任务超时，但 quiesce 等待 agent ack 的专用超时值未指定 | docs/04 quiesce 协议 + docs/12 环境变量 | 开发者可能将 quiesce timeout 设为与 task timeout 相同值，导致 quiesce 超时后无剩余时间执行 clone | 增加 `EXECUTOR_QUIESCE_TIMEOUT` 环境变量（建议默认 30s），并在 docs/04 中明确"quiesce 超时 MUST 小于 task timeout" | 架构组 | MVP 阶段 | **已修正** ✅ docs/04 超时策略 + docs/12 环境变量表同步增加 |
| P2-01 | P2 | 可观测 | 缺少 workspace 状态变迁指标：无法通过 metrics 观测 PROVISIONING→ACTIVE/INIT_FAILED 转换频率 | docs/09 Metrics 清单 | init_workspace 频繁失败时无法通过指标发现，需查日志 | 在 wvs-worker 指标中增加 `wvs_workspace_state_transitions_total{from, to}` counter | 开发组 | MVP/GA | **已修正** ✅ wvs-worker 指标表新增该 counter |
| P2-02 | P2 | 可观测 | 无文件系统空间/inode 监控：JuiceFS mount metrics 中有相关指标但 vmagent 未明确抓取 | docs/09 vmagent 配置 | 快照积累导致 inode 耗尽时无法提前预警 | 在 docs/09 明确列出需关注的 JuiceFS mount 原生指标（如 `juicefs_used_inodes`、`juicefs_used_space`） | 开发组 | MVP | **已修正** ✅ 新增 JuiceFS mount 原生指标关注清单（6 项含阈值建议） |
| P2-03 | P2 | 接口契约 | request_hash 计算公式不够精确：`SHA-256(canonical_request + method + path)` 中 `canonical_request` 未定义 | docs/06 幂等策略 | 不同开发者可能实现不同的规范化逻辑 | 明确定义 canonical_request 为 `sorted_json(request_body)`（按 key 字典序排列的 JSON 字符串） | 开发组 | MVP 阶段 | **已修正** ✅ 替换为 sorted_json 规范化定义 |
| P2-04 | P2 | 并发 | advisory lock 使用 `hashtext(wsid)` 产生 32-bit 整数，存在哈希碰撞概率导致不同 workspace 误串行 | docs/05 Advisory Lock + docs/06 并发互斥 | 高 workspace 数量场景下性能退化 | 记录为已知限制；GA 阶段可考虑使用 `pg_advisory_xact_lock(('x' \|\| md5(wsid))::bit(64)::bigint)` 替换 | 开发组 | GA |
| P2-05 | P2 | 可观测 | MVP demo 缺少自动化 smoke test 脚本：runbook 仅提供 curl 示例，无端到端自动化验收脚本 | docs/07 Runbook + docs/12 Demo 验收 | demo 验收依赖手工操作，不可重复 | 提供 `scripts/smoke-test.sh`，调用 wvsctl 执行完整流程并校验返回值 | 开发组 | MVP 阶段 | **已修正** ✅ 新增完整 smoke-test.sh 脚本规范（9 步）+ Makefile smoke-test target |
| P2-06 | P2 | 边界控制 | docker-compose.yml 混入 GA 组件（vmauth/vmalert/grafana/alertmanager）且无注释区分 | docs/12 docker-compose.yml | 开发团队可能误将 GA 组件视为 MVP 必选 | 拆分为 `docker-compose.yml`（MVP）和 `docker-compose.ga.yml`（GA override），或在文件中用注释块明确标注 | 开发组 | MVP 阶段 | **已修正** ✅ GA 组件添加 `profiles: ["ga"]` + 注释分隔块，`docker compose up` 默认仅启 MVP 组件 |

## 3. 逐项检查表（通过/不通过/条件通过）

### 3.1 功能闭环检查
| 检查项 | 结果 | 证据 | 备注 |
|---|---|---|---|
| 功能定义是否有接口承载 | 条件通过 | workspace CRUD、snapshot CRUD、set_current、task 查询均有对应 API 端点 | retry-init 未显式声明创建 `init_workspace` 类型任务，建议补充 |
| 接口是否有持久化字段支撑 | 条件通过 | workspaces/snapshots/tasks 表覆盖所有 API 返回字段 | P1-01：set_current 流程未提及更新 current_path 字段 |
| 是否存在死状态 | 条件通过 | workspace 状态机无死状态；task DEAD 为设计终态 | P1-02：CANCELED 状态不可达，无 API 触发路径 |
| 失败后是否有恢复路径 | 条件通过 | init_workspace 失败→retry-init；其他任务自动重试→DEAD 后人工介入 | P1-03：executor 层重试缺乏文件系统幂等，可能产生孤儿目录 |

### 3.2 接口契约检查
| 检查项 | 结果 | 证据 | 备注 |
|---|---|---|---|
| 前置条件可确定性校验 | 通过 | workspace state、snapshot 存在性、引用关系均可通过 DB 查询确认 | — |
| 错误码覆盖完整性 | 条件通过 | 13 个错误码覆盖常见业务冲突 | 建议补充：retry-init 状态不匹配→WVS_PRECONDITION_FAILED；set_current 目标 snapshot 不属于该 workspace→WVS_NOT_FOUND（应显式声明） |
| 分页契约一致性 | 通过 | 所有 list 接口统一使用 cursor + limit，默认 20，最大 100 | cursor 值的编码格式建议明确（如 opaque string） |
| 删除/禁用语义一致性 | 条件通过 | disable 同步 200，snapshot_drop 异步 202 | P1-05：disable 后资源可见性未定义；disable 的幂等行为未声明 |

### 3.3 并发与一致性检查
| 检查项 | 结果 | 证据 | 备注 |
|---|---|---|---|
| workspace 串行化覆盖 clone 冲突 | 通过 | pg_advisory_xact_lock(hashtext(wsid)) 保证同 workspace 串行 | P2-04：hashtext 碰撞可接受为已知限制 |
| 重试是否引入副作用重复 | 不通过 | 任务重试在 executor 层无幂等保障 | P1-03：需在 executor 增加目录/symlink 存在性检查 |
| deleted_at 与查询语义一致 | 通过 | list snapshots 仅返回 deleted_at IS NULL | — |
| current 更新无竞态漏洞 | 条件通过 | 在 advisory lock 内更新 current_snapshot_id | P0-01：但 snapshot_drop 的引用检查与 API 层任务插入之间存在竞态 |

### 3.4 可观测与可运维检查
| 检查项 | 结果 | 证据 | 备注 |
|---|---|---|---|
| MVP 指标可定位核心故障 | 条件通过 | HTTP 指标、任务指标、clone 指标、quiesce 指标覆盖主要故障域 | P2-01/P2-02：缺少 workspace 状态变迁指标和文件系统空间指标 |
| 日志链路字段完整 | 通过 | request_id、task_id、wsid 三字段可完成请求→任务链路关联 | — |
| 无监控盲区 | 条件通过 | 主要组件均暴露 /metrics | MinIO 健康状况在 MVP 仅通过 JuiceFS mount 间接观测，无直接指标 |
| demo 可重复验收 | 条件通过 | docker-compose + wvsctl 命令组定义明确 | P1-04：缺 migration job；P2-05：缺自动化 smoke test |

### 3.5 边界与范围控制检查
| 检查项 | 结果 | 证据 | 备注 |
|---|---|---|---|
| 无 scope creep 描述 | 条件通过 | 范围冻结条款有效，webhook 明确标注 GA | P2-06：docker-compose 混入 GA 组件可能引发 scope creep |
| MVP 不被 GA 阻塞 | 通过 | MVP 依赖链（JuiceFS+MinIO+PG+vmagent+vmsingle）独立于 GA 组件 | — |
| 无关键语义"开发者自由发挥"空间 | 条件通过 | MVP 固化决策 7 条覆盖核心场景 | P1-05：disable 后行为需固化；P1-06：quiesce timeout 需固化 |

## 4. MVP上线前置条件（Gate）
| Gate ID | 前置条件 | 结果 | 证据 | 备注 |
|---|---|---|---|---|
| G1 | 无未处置 P0 | **通过** ✅ | P0-01 已修复：snapshot_drop 执行顺序反转 | — |
| G2 | 主流程可实现并可演示 | **通过** ✅ | P1-01（current_path）、P1-04（migration job）均已修复 | — |
| G3 | 一致性与幂等规则闭环 | **通过** ✅ | P1-03 executor 层幂等已补充 | — |
| G4 | 可观测最小能力可用 | 通过 | vmagent + vmsingle + metrics + structured logs | — |
| G5 | Runbook 可执行 | 条件通过 | curl/wvsctl 示例覆盖主流程 | P2-05：建议提供自动化 smoke test |

## 5. GA延后项与触发条件
| 项目 | 延后原因 | 触发条件 | 预估工作量 | 依赖 |
|---|---|---|---|---|
| JWT/OIDC + RBAC | MVP 内网 demo 可无鉴权 | 面向外部用户暴露 API 或多租户需求 | 2-3 周 | IdP 选型 |
| PITR 编排（双库） | MVP 单实例容忍数据丢失 | 生产部署 + RPO < 60s 要求 | 2 周 | WAL-G 集成 + MinIO Object Lock |
| vmalert/Grafana 运营化 | MVP 使用 CLI 观测即可 | 需要持续运营看板和告警通知 | 1 周 | vmauth 配置 |
| wvs-index 一致性重建 | 仅 PITR 恢复后需要 | PITR 演练或生产恢复事件 | 1 周 | PITR 编排就绪 |
| 多 executor 调度 | MVP 单 executor 足够 | workspace 数量或 clone 负载超出单 executor 承载 | 1-2 周 | hash(wsid) 路由策略 |
| K8s 生产化 + NetworkPolicy | MVP 使用 docker-compose | 生产级 K8s 部署需求 | 2 周 | 安全基线确认 |
| Contract Test（OpenAPI/gRPC） | MVP 单元+集成测试足够 | API 版本演进、多团队协作 | 3-5 天 | OpenAPI spec 输出 |

## 6. 修订建议（精确定位）

| 建议ID | 文档位置 | 当前问题 | 建议文本（可直接替换） | 优先级 |
|---|---|---|---|---|
| R-01 | `wvs-tech-docs.md` docs/06-task-engine.md "关键业务规则" snapshot_drop 条目 | 引用检查与目录删除之间无原子性保障 | 增加规则："`snapshot_drop` 引用检查 MUST 在持有 workspace advisory lock 的同一事务内完成，并在同一事务内将 `snapshots.deleted_at` 设为 `now()`，**然后**由 executor 异步删除目录。若目录删除失败，MUST 记录告警但不回滚 `deleted_at`（反转当前先删目录后写 DB 的顺序）" | P0 | **已修正** ✅ |
| R-02 | `wvs-tech-docs.md` docs/01-architecture.md "Set Current" 第 4 步 | 仅提及更新 current_snapshot_id | 替换为："4. worker 更新 `workspaces.current_snapshot_id` **和 `workspaces.current_path`**" | P1 | **已修正** ✅ 同步修改了 docs/01 时序和 docs/06 关键业务规则两处 |
| R-03 | `wvs-tech-docs.md` docs/06-task-engine.md 状态机 mermaid 图 | CANCELED 状态存在但不可达 | 方案 A：移除 CANCELED 和 DEAD 状态中的 CANCELED，DDL CHECK 去除 'CANCELED'；方案 B：在 docs/04 增加 `POST /v1/tasks/{task_id}:cancel` | P1 | **已修正** ✅ 采用方案 B |
| R-04 | `wvs-tech-docs.md` docs/04-api-and-protocols.md DELETE /v1/workspaces/{wsid} | 未定义禁用后行为 | 在该端点下增加："**禁用后行为：** (1) 所有 GET 接口正常返回（只读）；(2) 所有写操作返回 `WVS_GONE 410`；(3) 重复 DELETE 幂等返回 `200`" | P1 | **已修正** ✅ |
| R-05 | `wvs-tech-docs.md` docs/12-cicd-and-deployment.md 环境变量 executor 段 | 缺少 quiesce 专用超时 | 增加环境变量：`EXECUTOR_QUIESCE_TIMEOUT`，默认 `30s`，说明："等待 agent ack FROZEN 的超时，MUST 小于 EXECUTOR_TASK_TIMEOUT" | P1 | **已修正** ✅ |
| R-06 | `wvs-tech-docs.md` docs/13-dev-guide.md executor 实现指引 | 无 executor 层幂等指导 | 在每个任务的执行步骤前增加："**幂等前置检查：** snapshot_create —— 若目标目录已存在且 `.wvs/snapshot.json` 完整，视为已完成；set_current —— 若 `current` symlink 已指向目标 live 目录，视为已完成；snapshot_drop —— 若目标目录不存在，视为已完成" | P1 | **已修正** ✅ |
| R-07 | `wvs-tech-docs.md` docs/12-cicd-and-deployment.md docker-compose | 缺少 migration 服务 | 在 wvs-api 之前增加服务定义（见下文完整 YAML） | P1 | **已修正** ✅ |
| R-08 | `wvs-tech-docs.md` docs/06-task-engine.md 幂等策略 | canonical_request 未定义 | 替换为："`request_hash = SHA-256(sorted_json(request_body) + method + path)`，其中 `sorted_json` 为按 key 字典序递归排列的 JSON 规范化" | P2 | **已修正** ✅ |
| R-09 | `wvs-tech-docs.md` docs/04-api-and-protocols.md retry-init | 未声明创建的任务类型 | 在响应下增加："创建 `op=init_workspace` 类型任务" | P2 | **已修正** ✅ 补充了 op 类型和 state 重置规则 |
| R-10 | `wvs-tech-docs.md` docs/01-architecture.md "Snapshot Drop" 步骤 | 当前顺序为先删目录后写 deleted_at（与 R-01 冲突） | 若采纳 R-01，需同步修改为："(1) worker 校验引用保护；(2) worker 在同一事务内写入 `deleted_at`；(3) executor 异步删除目录；(4) 目录删除失败记录告警" | P0 | **已修正** ✅ |

### R-07 补充：migration 服务 YAML

```yaml
  wvs-migrate:
    image: ${WVS_IMAGE_REPO:-yourorg}/wvs-api:${WVS_VERSION:-dev}
    entrypoint: ["migrate", "-path", "/migrations", "-database", "postgres://wvs:wvs_pass@pg-wvs:5432/wvs?sslmode=disable", "up"]
    depends_on:
      pg-wvs: { condition: service_healthy }
```

wvs-api 和 wvs-worker 需增加：
```yaml
    depends_on:
      wvs-migrate: { condition: service_completed_successfully }
```

## 7. 风险登记与跟踪

| 风险ID | 描述 | 概率 | 影响 | 缓解措施 | Owner | 状态 |
|---|---|---|---|---|---|---|
| RK-01 | snapshot_drop 与 set_current 并发竞态导致快照被误删 | 高 | 高 | 见 P0-01/R-01：反转执行顺序，先标记 deleted_at 后异步删除目录 | 架构组 | Open |
| RK-02 | quiesce 协议依赖 agent 正确实现，无服务端验证机制 | 中 | 高 | MVP：文档约束 + 集成测试覆盖；GA：考虑增加 lsof/inotify 验证 agent 确实停止写入 | 开发组 | Open |
| RK-03 | agent 虚假 ack FROZEN 后继续写入，导致快照不一致 | 中 | 高 | MVP：接受风险，文档标注为已知限制；GA：增加 mount-level 写入检测 | 开发组 | Open |
| RK-04 | executor 重试产生孤儿目录累积占用 inode/空间 | 中 | 中 | 见 P1-03/R-06：executor 幂等前置检查；定期扫描清理孤儿目录 | 开发组 | Open |
| RK-05 | MVP 无鉴权 + 误暴露到公网导致未授权操作 | 低 | 高 | 在 docs/03 和 docs/12 增加警告标注：MVP MUST NOT 暴露到公网；docker-compose 仅绑定 127.0.0.1 | 运维组 | Open |
| RK-06 | hashtext(wsid) 碰撞导致不同 workspace 互相阻塞 | 低 | 低 | 记录为已知限制，workspace 数量 < 10K 时碰撞概率可忽略 | — | Accepted |
| RK-07 | init_workspace executor 具体执行逻辑未文档化（目录结构创建、symlink 初始化） | 中 | 中 | 在 docs/13 增加 init_workspace executor 实现指引 | 开发组 | Open |

## 8. 最终签署
- 审查负责人签字：
- 业务负责人签字：
- 架构委员会签字：
- 签署日期：

## 9. 附录

### 附录 A：关键时序审查结论

**Snapshot Create 时序**：逻辑完整，quiesce → clone → 写 DB 闭环。需补充 executor 幂等检查（R-06）。

**Set Current 时序**：逻辑基本完整，但第 4 步漏更新 current_path（P1-01/R-02）。no-op 路径设计合理。

**Snapshot Drop 时序**：**存在 P0 级竞态**（P0-01）。当前"先删目录、后写 DB"的顺序在无竞态保护时不安全。建议反转为"先写 DB（标记 deleted_at）、后异步删除目录"，并在引用检查与 DB 写入之间保持 advisory lock 事务原子性。

### 附录 B：API 抽样用例与结果

| 用例 | 预期 | 审查结果 |
|---|---|---|
| 创建 workspace → snapshot → set_current → drop | 全链路可走通 | 通过（需修复 P0-01 后） |
| 重复 Idempotency-Key 相同请求体 | 返回原 task | 通过（幂等唯一约束支持） |
| 重复 Idempotency-Key 不同请求体 | 409 WVS_CONFLICT_IDEMPOTENT_MISMATCH | 通过 |
| drop 当前指向的 snapshot | 409 WVS_CONFLICT_SNAPSHOT_IN_USE | 通过 |
| set_current 到当前 snapshot | SUCCEEDED(noop=true) | 通过 |
| disable 有活跃任务的 workspace | 前置条件拒绝 | 通过（但响应码/错误码未明确） |
| 对已 disable workspace 执行写操作 | WVS_GONE 410 | 条件通过（需 R-04 补充后） |

### 附录 C：失败路径演算记录

| 场景 | 当前设计处理 | 是否充分 | 备注 |
|---|---|---|---|
| quiesce 超时 | 任务 FAILED，fail-closed | 充分 | 需明确超时值（P1-06） |
| clone 执行中 executor 崩溃 | JuiceFS mount 自动清理中断 clone 的 inode | 充分 | 文档已覆盖 |
| clone 成功但 DB 更新失败 | 任务保持 RUNNING → 超时 → FAILED → 重试 | 条件充分 | 重试时需 executor 幂等（P1-03） |
| symlink rename 失败 | 任务 FAILED → 重试 | 条件充分 | 旧 current 仍有效，需确认新 live 目录是否需清理 |
| worker 持锁期间崩溃 | 事务级 advisory lock 随连接断开自动释放 | 充分 | — |
| snapshot_drop 目录删除失败 | 任务 FAILED，不写 deleted_at | 充分 | 重试会再次尝试删除 |
| init_workspace 达到最大重试 | workspace state → INIT_FAILED，用户调用 retry-init | 充分 | — |
| 并发 set_current 同一 workspace | advisory lock 串行化 | 充分 | — |
| 并发 snapshot_drop 与 set_current | **竞态窗口** | 不充分 | P0-01 |
