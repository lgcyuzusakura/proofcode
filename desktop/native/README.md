# ProofCode Native

Windows Wails 应用，直接导入共享 React 工作台。`frontend/dist` 在构建后嵌入 Go 二进制。

原生桥接支持桌面工程幂等创建、本机源码快照、受控补丁应用、项目隔离和事件轮询，并提供 Monaco编辑/版本核对保存、真实命令控制台、Git管理与独立Edge/Chrome预览。主界面以对话为中心，审查在独立页面。详见[操作与边界](../../docs/developer-workspace.md)。源码上传排除凭据、Git 内部目录、缓存目录和链接；补丁应用会校验基线与文件并发变化。

```powershell
$env:PROOFCODE_CONTROL_PLANE_URL = "http://127.0.0.1:8080"
.\desktop\start-native.ps1
```

地址来自原生进程环境变量，默认 `http://127.0.0.1:8080`。在界面设置中填写访问令牌；模型 API key 仍由 Runner 配置。API 绑定限制路径、方法、头部及消息大小。

本机路径仅保存在用户配置目录 `ProofCode/projects.json`。`BootstrapScratchProject` 使用 Windows Known Folder 获取真实桌面；`CaptureProjectSource` 尊重 `.gitignore`，包括非 Git 工程，且不会在用户目录创建 `.git`。

快照每文件最大 1 MiB，总计最大 8 MiB/2000 文件，不支持链接和子模块。`ApplyProjectPatch` 在私有 Git 仓库校验补丁，在 Windows 独占文件句柄内比较旧内容与写入，新文件使用 `CREATE_NEW`，删除也校验旧内容。

多文件修改不是文件系统事务。恢复日志中的前/后镜像无法匹配断电后的部分字节时，恢复停止，保留备份供人工处理。非 Windows 平台暂不支持自动应用补丁。

## Live Development

需要 Node/npm、Go 1.24或更新版本、Wails 2.10.2 和 Windows WebView2。在本目录运行 `wails dev`。构建脚本支持 `PROOFCODE_GO_ROOT` 和 `PROOFCODE_WAILS_BIN`，默认从仓库 `.tools/` 寻找工具。

## Building

在仓库根目录运行 `.\desktop\build-native.ps1`，或在本目录运行 `wails build -clean`。构建结果位于被 Git 忽略的 `build/bin/`。

```powershell
Push-Location frontend
npm ci
npm test
npm run build
Pop-Location
go test ./... -count=1
```
