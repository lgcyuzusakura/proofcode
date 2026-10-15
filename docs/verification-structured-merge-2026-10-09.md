# 2026-10-09 结构化合并与 IDE 工具验收

分支：`codex/structured-merge-debug-tools`。本记录是工程验证，不是论文模型效果数据。

## 验证内容

- Git 独立函数修改、未批准工作区/index/HEAD 隔离、双父 merge commit 和原 HEAD 备份。
- Mergiraf 组合同行独立语法修改，同一表达式的真实冲突保留人工解决。
- stale preview、目标移动、跨项目、预览 index 修改和受保护路径拒绝。
- 真实 pylsp initialize、didOpen、hover，以及 Monaco 诊断和补全。
- 真实 debugpy 断点停止、堆栈、作用域、变量、表达式求值、单步与继续。
- Chromium bundled DevTools tabs 和 SDK，连接真实项目 target 并在 Runtime 读取页面内容。

## 回归结果

以下命令均在 2026-10-09 完成并返回成功：

```powershell
cd desktop/native
go test ./...
cd ../../frontend
npm test
npm run build
cd ..
./desktop/build-native.ps1
```

额外的真实桌面 UI 验收使用临时控制平面、Edge、Playwright、pylsp 1.13.1、debugpy 1.8.17 和 Mergiraf v0.20.0，确认编辑器保存/草稿恢复、Git 合并审批、LSP 诊断与补全、gutter 断点、DAP 堆栈/变量/求值/单步、命令、浏览器、审查跳转和移动布局；结果为 `PASS`，页面错误数为 0。截图保存在 `docs/images/structured-merge.png` 和 `docs/images/lsp-debug.png`。

当前验证不包含真实模型任务集效果，不把 Token 为 0 的确定性合并路径扩展成整个 Agent 的 Token 节省百分比。

## 依赖版本

Mergiraf v0.20.0 为外部可选 GPL-3.0-only 进程，不随仓库提交。测试 Python 环境为 python-lsp-server 1.13.1、debugpy 1.8.17、pyflakes 3.4.0；不修改系统 Python。

## 边界

尚无完整 LSP workspace edit/rename/format、DAP reverse terminal、远程 TCP adapter、扩展市场、Git rebase/stash。DevTools 为独立 Chromium 自带窗口，页面预览仍是按操作更新的截图。详见[专项说明](structured-merge-and-debugging.md)。
