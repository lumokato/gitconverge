# GitConverge

GitConverge 是面向 Windows 的多仓库 Git 自动收敛应用。安装后通过托盘打开本机管理界面，在前端添加仓库、调整同步策略、查看状态和运行记录；不需要手工编写配置文件或使用命令行完成日常设置。

GitConverge 使用系统 Git CLI，在明确的安全边界内完成 fetch、非强制 push、fast-forward 和可选三方合并。它适合让多台 Windows 设备上的普通 Git 仓库持续靠拢，同时保留 Git 历史、冲突现场和人工决策权。

## 主要特性

- 一个应用管理任意数量的现有 Git 工作目录。
- 前端添加、修改和移除仓库，配置远端、分支、抓取周期和分叉策略。
- 本地领先时执行非强制 push；本地落后且工作树干净时执行 fast-forward。
- 分叉可选择停止并提醒，或尝试三方合并；真实冲突会中止合并并保留现场。
- 工作树有未提交修改时，仍可传输已有提交，但不会自动 fast-forward 或合并远端变化。
- 托盘显示运行状态，并可打开管理界面或立即同步。
- “运行记录”页面显示有限、脱敏的本机历史，默认保留 14 天且不超过 10 MiB。
- 可选钉钉机器人通知；凭据保存后不会在前端回显。
- Git 和 SSH 命令具有硬超时；Windows 使用 Job Object 清理超时后的子进程树。

GitConverge 不执行 `reset --hard`、`clean`、force push、rebase，也不会在冲突时擅自选择任意一方。

## 安装和使用

1. 从 GitHub Releases 下载 `GitConverge-<版本>-windows-x64-setup.exe`。
2. 双击安装。程序安装到当前用户目录，不需要管理员权限。
3. 安装完成后 GitConverge 自动启动，并打开首次配置页面。
4. 点击“添加仓库”，选择现有 Git 工作目录，确认远端、分支和同步策略。
5. 点击“保存全部”。正式安装版本会自动创建 Windows 周期同步任务。

之后可从系统托盘打开 GitConverge：

- **概览**：查看仓库总况、异常、脏工作树和最近动作。
- **仓库**：添加或修改同步仓库和安全策略。
- **运行记录**：查看每轮结果、耗时、仓库动作和脱敏错误摘要。
- **设置**：调整同步周期、并发、超时、日志保留和通知。

卸载程序会删除 GitConverge 程序、托盘自启和计划任务，但保留配置与运行状态，便于重新安装或人工检查。

## 安全边界

- 本地管理界面只监听 `127.0.0.1` 的随机端口，并使用每次启动生成的随机 token。
- 状态和配置接口不返回 webhook、secret 或环境变量值。
- 运行记录对常见凭据内容进行脱敏，单条错误摘要限制长度。
- `state.json` 保存当前状态，`runs.jsonl` 保存有限历史；超过时间或大小限制时删除最旧记录。
- `fallback_commit` 默认关闭。启用前应确认仓库的 `.gitignore` 已排除凭据、缓存和不应进入历史的大文件。

## 从源码构建

需要 Go 1.25、系统 Git 和 Inno Setup 6：

```powershell
pwsh -NoProfile -File .\scripts\build-release.ps1 -Version 0.1.0
```

生成内容：

- `dist\windows-amd64\gitconverge.exe`
- `dist\windows-amd64\gitconverge-tray.exe`
- `dist\GitConverge-0.1.0-windows-x64-setup.exe`

开发和自动化场景仍可使用 CLI：

```powershell
gitconverge check --config C:\path\to\config.toml
gitconverge run --config C:\path\to\config.toml
gitconverge status --config C:\path\to\config.toml --json
gitconverge dashboard --config C:\path\to\config.toml
```

配置仍保存为可迁移的 TOML 文件，默认位于 `%APPDATA%\GitConverge\config.toml`；日常使用无需直接编辑。
## 许可证

GitConverge 使用 [Apache License 2.0](LICENSE)。随二进制分发的第三方组件及其原始许可证见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) 和 [third_party_licenses](third_party_licenses/)。
