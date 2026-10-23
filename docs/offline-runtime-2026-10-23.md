# Windows 离线服务包（2026-10-23）

[`v0.4.1-20261023`](https://github.com/lgcyuzusakura/proofcode/releases/tag/v0.4.1-20261023) 对应应用提交 `65913c5`。目标系统为 Windows x64，Docker Desktop 必须使用 Linux/amd64 容器；桌面界面仍需 WebView2，本机 Git 操作仍需 Git。包内没有 Docker 安装器、用户数据库、模型或 API Key。

## 下载与安装

1. 下载该发行版的 Windows 客户端 ZIP、`ProofCode-Offline-Setup.zip`、全部四个 `ProofCode-Offline-Images-01-of-04.zip` 至 `04-of-04.zip`，以及 SHA256SUMS 校验文件。四片缺一不可。
2. 将 Setup ZIP 和四个 Images ZIP 解压到同一个新文件夹；里面应有 `install.ps1`、`compose.yml`、`proofcode-offline.json`、`README.txt` 及四份 `images.tar.part00*`。
3. 在这个文件夹打开 PowerShell，选择一个尚不存在的部署目录，执行：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1 -Package . -Destination D:\ProofCode-runtime
```

4. 解压 Windows 客户端 ZIP，启动 `ProofCode.exe`，打开“运行环境”，选择刚生成的 `D:\ProofCode-runtime`，点击“启动服务”。如端口占用，可安装时使用 `-ControlPort 28080 -WebPort 23000`，并在桌面后端连接设置使用对应 API 端口。
5. 注册或登录后配置自己的模型地址、协议、模型名和 API Key。此处仍会访问所选模型服务；离线镜像只免除应用镜像构建与拉取，不能代替模型网络。

安装脚本验证每个分片和重组归档的 SHA-256，加载后检查全部六个镜像的内容 ID 与平台，再为新目录独立生成本机密钥。脚本不会启动服务或覆盖旧目录；已有 `.env` 必须妥善备份，开始使用后不能随意更换数据加密密钥。写文件和执行命令默认关闭，可按实际任务权限调整部署配置。

桌面检测到离线清单时执行 `up -d --no-build --pull never`，缺少或不匹配的镜像会明确失败。停止只执行 `stop`，保留数据卷；部署配置修改后需重新选择目录。该目录仍是用户信任的 Compose 配置，不是运行不可信配置的沙箱。

## 验证记录

- Windows PowerShell 5.1 实际导入四个分片，归档总大小 2,362,285,568 字节；分片 ZIP 使用压缩传输，展开后的内容 hash 与清单一致。
- 六个服务实际启动，五个配置的 Docker 健康检查通过，Runner 处于 running；Web 与 API 健康端点返回成功。
- PostgreSQL 写入测试标记后 `stop`、离线重启并再次查询，数据保留；测试项目容器、网络及临时卷已按独立标签清理。
- 桌面验证使用实际 Docker 和完整导出清单；包含拒绝坏清单、错误镜像、构建指令与拉取策略的测试。Go 测试与 vet、三个 Python 打包测试通过。
- 应用提交的 [Java、Go、前端与 Windows CI](https://github.com/lgcyuzusakura/proofcode/actions/runs/39898303567) 全部通过。真实模型编程质量与完整独立安装器仍未完成验证。

源码档案仅从清理后的 Git 历史导出，镜像只从代码白名单构建；不包含本机 `.env`、Git 历史、论文或私有备份。
