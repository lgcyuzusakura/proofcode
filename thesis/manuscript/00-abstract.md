# 摘要

编程智能体能够根据自然语言需求读取仓库、修改代码并执行测试，但持续任务中的版本混淆、重复工具日志和外部数据写入会增加结果核验与恢复难度。针对这些问题，本文设计并实现面向可验证代码修改的ProofCode系统核心后端。系统采用Go执行引擎与Java Spring Boot控制平面，使用PostgreSQL保存任务及批准事实，以事务Outbox和ActiveMQ Artemis实现任务派发，并在独立Git工作树中进行代码修改和验证。

上下文模块采用词法、符号、文本依赖线索和测试四路检索，通过倒数排名融合组织原文证据，并以项目、工作区、任务尝试、Git提交及文件哈希绑定证据版本。压缩模块保留完整原始会话，只在模型输入副本中替换完全重复的成功纯日志。数据网关通过模式快照、受限查询中间表示、参数化SQL、计划摘要和用户批准约束数据库操作，并提供PostgreSQL事务回滚和Redis条件补偿。系统同时实现六组固定输入的执行配置及结果比较接口。

验证采用软件测试、服务全链路夹具和合成机制检查。六组夹具均完成真实补丁与统一测试；真实PostgreSQL工作流的批准前修改次数为0，批准后为1，重复和拒绝未增加修改。重复成功日志在合成条件下可缩短模型视图，而全链路夹具未出现去重收益。由于模型与决策接口使用固定响应，本文结果证明工程机制及执行框架的可复现性，不代表真实模型的任务能力提升。后续需通过真实任务集进一步评价检索、压缩与决策路由的效果。

关键词：编程智能体；版本感知检索；上下文压缩；数据库审批；可复现验证

# Abstract

Coding agents can inspect repositories, edit code, and execute tests in response to natural language requests. However, inconsistent code versions, repeated tool logs, and external data mutations make long running tasks difficult to verify and recover. This thesis presents the core backend of ProofCode, a system for verifiable code modification. A Go execution engine works with a Java Spring Boot control plane. PostgreSQL stores task and approval records, a transactional outbox and ActiveMQ Artemis dispatch tasks, and isolated Git worktrees contain code changes and verification results.

The context module combines lexical, symbol, textual dependency, and test retrieval through reciprocal rank fusion. Source evidence is bound to project, workspace, task attempt, Git revision, and file hashes. The compression module preserves the original transcript and replaces only identical successful plain logs in the model input view. A project scoped data gateway uses schema snapshots, a restricted query intermediate representation, parameterized SQL, plan digests, and explicit user approval. PostgreSQL transactions and separately approved Redis compensation handle supported failure cases. Six executable configurations share fixed inputs and a result comparison interface.

Verification includes software tests, a service level fixture, and synthetic mechanism checks. All six fixture configurations applied real patches and passed a common test command. In a real PostgreSQL approval workflow, the mutation count was zero before approval and one afterwards; replay and denial caused no additional mutation. Repeated successful logs reduced the estimated model view under synthetic conditions, while the service fixture showed no deduplication benefit. Since model and decision responses were fixed, the results establish reproducibility of the engineering mechanisms rather than gains in model task performance. Further evaluation requires real tasks and labeled retrieval data.

Keywords: coding agent; version aware retrieval; context compression; database approval; reproducible verification
