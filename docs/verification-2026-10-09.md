# 2026-10-09 实现验收记录

对应 `codex/project-workspace-bootstrap`。这是工程验收，不是正式论文模型性能实验；所有生成模型与 Jev 全链路响应均来自明确标记的固定夹具，不能据此声称低幻觉、任务成功率或性能优于其他 Agent。

## 已完成

- 项目、工作区、对话归属链与消息持久化；CHAT 无执行工具，连续对话恢复历史。
- Windows 未导入目录时创建桌面工程；原生端保持真实路径，控制平面只保存不透明 handle 与相对路径快照。
- 脏文件和未跟踪文件捕获、哈希绑定、隔离任务源码、下一轮源码继承；同 SCRATCH 工作区普通代码任务互斥。
- PostgreSQL 类型化单表查询、字段拖动、嵌套筛选、排序分页、行版本约束编辑、受限事务 migration。
- Redis 项目／连接／环境命名空间、字符串读取和修改、TTL、分页 SCAN、版本与条件恢复。
- 独立数据管理会话、不可变计划、显式批准、一次性执行资格、审计、加密恢复快照；未知提交结果不会自动重放。
- 版本源码 manifest、Go AST／文本回退、倒排索引、分层带权 RRF、明确历史版本选择、原文证据回取及持久压缩引用。
- 实际执行的 A–F 六种 profile，协议 `proofcode.experiment.v2-context`，导出 JSON／CSV。
- Web／Wails 共用工作台，原生 HTTP API 代理、事件轮询、Token 设置；简中、英文、繁中和日文说明同步。

## 本机验证

| 检查 | 结果 |
| --- | --- |
| Go 引擎 `go test ./... -count=1` | 全部测试包通过 |
| Java `mvn -q test`，开启 live data tests | 12 个 suite、69 项，失败 0、错误 0、跳过 0 |
| 真实 PostgreSQL／Redis adapter tests | 9 项通过，包含失败回滚、条件补偿及冲突保护 |
| 原生 Go `go test ./... -count=1` | 通过，含独占句柄、编辑冲突、创建／删除和恢复 |
| 前端 `npm test` | 16 项通过 |
| Web `npm run build` | TypeScript 与 Vite 通过 |
| Windows `desktop/build-native.ps1` | Wails 2.10.2 production windows/amd64 构建成功 |
| PostgreSQL Flyway | V1–V15 均 success=true |
| 固定夹具全链路 | 最新运行通过，约 13.546 秒；只代表夹具执行时长 |
| Edge 实际界面 smoke | PostgreSQL 查询／EXPLAIN／批准／执行，Redis SET／GET 均通过，无 pageerror |
| 手机布局 | 390×844，document width=viewport width=390，无横向溢出；默认折叠导航 |

全链路断言包括：六组配置互不相同且实际应用；同一固定 commit 的真实 patch 和独立测试；数据库批准前写入计数为 0，批准后为 1，重复与拒绝后仍为 1；两轮聊天有四条持久消息；新建 Python 文件并实际运行 pytest；下一轮继承上一轮源码；无修改也保存输出快照；跨项目源码拒绝。

界面测试只使用独立实验服务的 PostgreSQL／Redis，不连接用户生产数据。测试输出和截图留存在本机备份目录；可重复的全链路脚本是 `smoke/experiments/verify.py`。

## 发现并修复的问题

1. 历史 assistant 消息误消耗新一轮 MaxSteps：分离聊天历史与当前 attempt 检查点，按最新 user 起算。
2. 无源码修改时丢失结果快照：成功路径仍保存源码绑定；重试清除旧输出，上传结果核对 runner、attempt、状态和租约。
3. 同工作区并发结果继承可能覆盖：SCRATCH 普通 CODE 创建与重试在项目锁内检查活跃任务，幂等请求先返回原任务。
4. 本机应用的比较／替换竞争：Windows 独占句柄内复核哈希并写入，拒绝重解析点、多硬链接和并发编辑；多文件恢复保留日志。
5. 非 Git 目录忽略规则遗漏：使用临时 Git 元数据执行嵌套 ignore 判断，不创建或修改原目录 `.git`。
6. Windows CRLF 容器入口失败：shell 固定 LF，镜像构建归一化入口脚本。
7. 连接 UI 发 schema 数组而接口只接受字符串：服务端兼容有界字符串数组和旧字符串，验证标识符；Redis 接受空数组。
8. 夹具错误发送 `run_command.command`：改成实际工具的 `program`／`args`，保持直接程序执行验证。

## 当前边界

尚无团队 RBAC 和每任务网络／CPU／内存隔离，不能宣称完成企业生产认证。PostgreSQL 查询不支持 JOIN／聚合；Redis 限 standalone string；已提交 migration 不做通用自动逆变换。检索没有 embedding，其他语言语法分块为文本回退。Token 预算是字节估算。原生源码多文件应用通过日志恢复，不是文件系统事务。共享工程更早快照的重试仍使用原始输入，应审阅结果或另建工作区。真实模型性能与正式论文 A–F 数据须另行采集。
