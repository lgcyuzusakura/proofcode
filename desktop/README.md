# ProofCode 桌面入口

Run from PowerShell at the repository root:

```powershell
.\desktop\start-desktop.ps1
```

原生 Windows 应用与 Web 使用 `frontend/src/WorkspaceApp.tsx` 中的同一套界面。项目树按 Project → Workspace → Conversation 展示，支持聊天和代码任务；数据库、Redis、批准和审计按项目隔离。

原生应用通过受限 Go HTTP 绑定访问控制平面，使用可恢复事件轮询。无目录对话会在 Windows 桌面创建 `ProofCode-Projects/<名称>_<身份>/`；网络失败与重启都可用同一身份恢复。代码任务捕获未提交源码，以校验清单上传；补丁应用遇到文件被编辑器占用或已改变时拒绝，保留本机恢复日志。

登录前可进入“运行环境与本机服务”，保存后端根地址。新构建包含源码运行包，可点击“准备本机运行文件”创建新部署目录并自动生成本机配置，也可选择已有 Compose 目录；启动与停止均需显式操作，停止保留数据卷。宿主仍需 Docker Desktop、Git，首次服务构建需要网络；2026-10-22 的 Windows 客户端已按本轮源码重新构建，分发与依赖说明见 [更新报告](../docs/update-report-2026-10-22.md)。详见[运行管理](../docs/agent-platform-runtime.md)。
