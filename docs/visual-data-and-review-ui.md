# 可视化数据与审查界面设计

日期：2026-10-10。本文记录 ProofCode 本轮界面调整的依据和边界。

## 参考方向

- [Linear](https://linear.app/features)：使用简洁的导航、明确的状态层级和较少的视觉噪声，作为主工作台与执行链的信息层级参考。
- [Supabase Studio](https://github.com/supabase/supabase/tree/master/apps/studio)：使用表结构、表数据和操作面板组织数据库工作流，作为 PostgreSQL 结构浏览和查询画布的参考。
- [Redis Insight](https://redis.io/insight/)：强调键空间浏览、键值预览、TTL 和数据类型，作为 Redis 可视化操作台的参考。
- [React Flow](https://github.com/xyflow/xyflow)：作为后续表关系图和 Agent 执行图的交互方向参考。本轮使用轻量 CSS 节点布局，没有新增图编辑器依赖。

这些项目只提供交互与信息架构参考，没有复制它们的代码、品牌或视觉素材。

## 本轮调整

数据库页顶部新增项目级总览：连接数、已发现表、待审批计划和已完成操作数。下方的四步状态带把“选择连接 → 读取结构 → 生成计划 → 审批执行”固定成用户能快速理解的路径。完成数只来自服务端成功状态，不把未知结果算作完成。

PostgreSQL 结构面板新增表节点概览和可缩放、可搜索的表关系图；关系来自外键元数据，每次最多显示 30 张表。选择表后再查看字段并拖入查询画布。行编辑以字段表单呈现，主键与生成列保持锁定。Redis 页面把技术操作改为五个中文操作卡片，键空间、TTL、大小和键值预览分别展示，原始计划参数仍可在详情中查看。

计划检查器默认展示目标环境、中文操作摘要、资源绑定和三步状态。完整 SQL、Query IR 和完整摘要默认折叠；查询的 EXPLAIN 可以主动请求。执行链使用中文时间线和可选择节点详情，逐 Token 事件和用量更新不占据时间线。完整事件 JSON 保留在折叠详情中。工具审批以中文工具名和参数字段呈现，未知风险不会默认为低风险。

## 安全语义保持不变

这些修改只改变 React 呈现层，不改变服务端计划摘要、一次性执行资格、用户批准、项目隔离、回滚和审计逻辑。界面显示“执行成功”前仍以服务端返回的状态为准；`COMMIT_UNKNOWN`、失败回滚和拒绝状态不会被渲染为成功。

## 验收证据

- `npm run build`：TypeScript 和生产构建通过。Monaco 相关大包仍有 Vite 体积提示。
- `npm test`：原有 16 项契约测试及新增 2 项呈现边界测试通过。
- [Playwright 验收脚本](../smoke/visual/ui.cjs)：1440 × 1000 桌面及 390 × 844 窄屏，无页面 JavaScript 异常，文档无横向溢出。
- 真实本地服务完成项目注册、连接注册、结构读取、Redis SCAN 计划生成、批准与执行；批准之前执行请求数为 0。
- 为查看非空界面，脚本在浏览器请求层注入了 `visual_orders` 外键、执行事件与工具审批样例。这些是合成视觉数据，没有写入数据库，没有调用 LLM，也不作为论文实验结果。

截图：[数据库关系图](images/visual-database-schema.png)、[Redis 操作台](images/visual-redis-workbench.png)、[代码审批](images/visual-review.png)、[执行链](images/visual-execution-chain.png)、[窄屏执行链](images/visual-mobile.png)。

复现时先启动测试控制平面及其 `SMOKE_DB`、`SMOKE_REDIS` 数据源，再启动前端。脚本依赖 Playwright 与 Edge，使用以下环境变量指定当前环境：

```powershell
$env:PROOFCODE_PLAYWRIGHT_MODULE = "<Playwright 模块路径>"
$env:PROOFCODE_UI_CONTROL_URL = "http://127.0.0.1:18097"
$env:PROOFCODE_UI_URL = "http://127.0.0.1:15175"
$env:PROOFCODE_UI_TOKEN = "<测试用户令牌>"
$env:PROOFCODE_UI_OUTPUT = "<验收结果目录>"
node smoke/visual/ui.cjs
```

## 后续可视化工作

1. 为表关系图增加节点拖动与关系边定位；当前支持搜索、缩放和选择表。
2. 在键空间加入类型分布、TTL 分布和内存占用趋势图，数据来源必须是已批准的只读统计计划。
3. 为执行链增加按步骤的耗时、输入输出 Token 和验证结果小图表；原始事件仍保留在详情抽屉中。
