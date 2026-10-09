# ProofCode 桌面入口

Run from PowerShell at the repository root:

```powershell
.\desktop\start-desktop.ps1
```

原生 Windows 应用与 Web 使用 `frontend/src/WorkspaceApp.tsx` 中的同一套界面。项目树按 Project → Workspace → Conversation 展示，支持聊天和代码任务；数据库、Redis、批准和审计按项目隔离。

原生应用通过受限 Go HTTP 绑定访问控制平面，使用可恢复事件轮询。无目录对话会在 Windows 桌面创建 `ProofCode-Projects/<名称>_<身份>/`；网络失败与重启都可用同一身份恢复。代码任务捕获未提交源码，以校验清单上传；补丁应用遇到文件被编辑器占用或已改变时拒绝，保留本机恢复日志。
