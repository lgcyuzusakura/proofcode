# 附录A 复现入口与证据索引

## A.1 源码与命令

ProofCode公开仓库与本章使用的固定后端提交见参考文献[34]。工程夹具、机制检查与正式模型实验采用不同入口，运行前应确认服务环境及数据用途。Compose工程夹具只使用可丢弃测试资源；它的固定模型响应不代替真实模型研究。

```powershell
# 仓库根目录：Go测试
go -C agent-engine test ./...
go -C agent-engine vet ./...

# control-plane目录：常规Java测试
mvn -s settings.xml test

# 仓库根目录：服务级工程夹具
docker compose -f compose.experiments.yml up --build -d runner
docker compose -f compose.experiments.yml run --build --rm verify

# 仓库根目录：合成检索与去重检查
powershell -File thesis/tools/run_mechanism_bench.ps1
```

正式实验通过POST /api/experiments创建，传入固定模型、完整commit、问题、测试命令和重复次数。C/F需要有效Jev服务。GET /api/experiments/{id}/compare可导出逐Run JSON或CSV。生成结果后仍须按实际模型、任务来源、配置失效和缺失字段进行解释，不能仅凭接口返回成功补齐论文指标。

## A.2 证据文件

本文的资料结构与Word、PDF归档形式参照长春大学教务处公开的2024届工作通知及其附件[32-33]。下面的索引将复现实验所需材料与论文叙述对应，便于核查来源和适用范围。

| 文件 | 记录内容 | 可以支持的结论 |
| --- | --- | --- |
| evidence/fixture-summary.json | 服务夹具summary副本 | 六组与真实PostgreSQL批准流程 |
| evidence/fixture-compare.csv | 六条逐Run记录 | 观测值、缺失值和固定提交 |
| evidence/mechanism-results.json | 合成日志与检索结果 | 去重、原文、版本和缓存 |
| evidence/test-report-summary.json | Surefire统计和哈希 | 35条通过、2条跳过 |
| evidence/provenance.json | 文件哈希和后端基线 | 证据完整性与版本追踪 |

# 附录B 参考项目与借鉴范围

## B.1 编程智能体项目

参考项目用于了解执行循环、编辑协议、工作区和批准设计。借鉴设计思路与直接复用依赖应区分，ProofCode没有继承这些项目的评测结果。参考库部分副本来自2026年9月29日的HEAD压缩包，缺少冻结commit，因此本文使用项目名称和访问日期描述参考范围，不以当前主页内容推定该历史副本的所有功能。

| 项目 | 文献编号 | 本课题参考内容 |
| --- | --- | --- |
| Codex | [16] | 权限批准、执行边界和任务事件 |
| Claude Code | [17] | 终端编程工作流；公开仓库不等于完整开源核心 |
| OpenCode | [18] | 会话、Provider、工具与客户端组织 |
| Aider | [19] | RepoMap、编辑格式和Git变更审查 |
| OpenHands | [20] | 执行环境与任务组织，注意版本变化 |
| mini-swe-agent | [28] | 模型—动作—观察的简洁循环 |
| Pi | [29] | 消息、工具、事件与会话模块划分 |
| Crush | [30] | Go工程组织、会话和权限 |
| Cline | [31] | 可见工具结果和用户批准交互 |

部分项目使用商业或特定许可证，公开可访问不表示可以任意复制。本文没有以项目星数或营销描述作为功能优越性依据。实际Go依赖由go.mod固定，Java依赖由pom.xml固定；检索中的RRF与上下文相关方法则按研究文献列出。

## B.2 数据库与上下文项目

Bytebase[21]提供数据库变更治理参考；ChartDB[22]和drawDB[23]提供结构画布与建模参考；DBeaver[24]提供数据源和查询界面参考；React Flow[25]是未来节点画布的候选组件。它们支持本项目后续可视化设计，不构成当前后端已经实现拖拽数据库界面的证明。LLMLingua及其后续研究[6-7]提供压缩方法参考，当前实现未引入其模型或压缩推理代码。

# 附录C 指标观测与研究边界

## C.1 Agent与检索指标

| 指标类别 | 当前可采集数据 | 尚需补充的研究证据 |
| --- | --- | --- |
| 工具与策略 | 调用、错误、无效调用、策略拦截计数 | 真实任务分母及风险样本标签 |
| 任务和测试 | Task终态、实际验收命令结果 | 独立验收、真实任务完成定义 |
| 用量与时延 | 完整usage或unknown、durationMs | 真实模型请求、审批等待分解和足量样本 |
| 恢复与资源 | 检查点和attempt机制 | 断线故障实验、显存峰值采集 |
| 检索排名 | 原文证据、快照、排名及缓存 | Recall@K、MRR、NDCG、文件与行标签 |
| 压缩影响 | 估算前后、去重数、原记录保持 | 真实usage、配对任务验收与压缩错误 |

## C.2 数据库指标

自然语言到Query IR准确率需要带目标IR的任务标签；IR到SQL执行正确率需要独立业务验收；EXPLAIN通过与危险操作拦截、误报率需要分别定义合法及危险样本。当前真实批准工作流观测到批准前修改次数为0，但审计完整率、恢复成功率和未知提交处理质量仍需专项任务集。Migration依赖DDL迁移实现，当前没有该功能，不能统计其回滚成功率。缺失字段保留unknown，不能用零值表示未执行的研究。
