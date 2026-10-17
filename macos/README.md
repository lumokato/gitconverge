# GitConverge macOS 桌面版

此包为 Intel x86_64 原生构建，包含菜单栏 `.app`、Go worker、现有本机管理界面和第三方许可证。Apple Silicon 未经过实机验收，不宣称支持。构建最低目标 macOS 12；实际测试版本以 Release notes 为准。

## 安装和升级

从 GitHub Release 下载 ZIP 及 `.sha256`，在 ZIP 所在目录执行 `shasum -a 256 -c <文件>.sha256`，解压后运行 `./install.sh`。首次安装先编辑 `config.macos.example.toml`，再运行 `./install.sh /绝对路径/config.toml`。安装到 `~/Applications/GitConverge.app`，worker 为 `~/.local/bin/gitconverge`，不会更改 SSH 或仓库内容。升级前从菜单退出 UI，等待同步空闲；若存在活动同步，安装拒绝继续，不杀同步进程。

此程序没有 Apple Developer ID 签名或公证，仅有本地 ad-hoc 签名。下载后可能被 Gatekeeper 阻止；按系统提供的“仍要打开”入口仅批准此应用。不要全局关闭 Gatekeeper，不需要 sudo，不购买开发者账号。若系统策略禁止，保留阻塞并向管理员申请单应用许可。

## 菜单与后台职责

- 模板菜单栏图标随深浅色主题调整。菜单显示同步中、暂停、调度未加载或最近错误；“空闲”不表示实时远端健康，最近完成时间单独显示。
- 管理界面在应用内 WKWebView 打开，地址栏不显示 token；复用仓库/设置/运行记录界面。HTTP 仅监听 `127.0.0.1` 随机端口并要求随机 token。界面含本地路径，分享截图前必须脱敏。手动输入目录可用，Mac 文件选择器可用。
- “立即同步”调用同一 worker，全局锁防止与自动任务并行；短时间重复点击也受桥接层防重复保护。
- “暂停”持久保存私有 pause 标记，仅阻止自动同步，不中断正在完成的同步；手动同步仍可用。“恢复”取消标记，等待下次计划或点击立即同步。
- `com.gitconverge.worker` 是唯一同步调度，默认600秒；菜单栏的4秒状态刷新不触发同步。`com.gitconverge.menubar` 只负责登录启动 UI，不含周期定时器，也不自动重启已退出的 UI。
- “退出菜单栏（同步继续）”只关闭 UI/HTTP 桥接，不停 worker 或计划。暂停状态在退出后保持。重新打开应用不会建立第二个菜单栏实例。
- 睡眠/注销时不保证同步，登录配置可检查；未实际注销测试时不得声称冷启动验收完成。

## 备份与回滚

安装前保存 app、worker、配置、两个 plist 和状态目录到 `~/Library/Application Support/GitConverge/desktop-backups/<timestamp-pid>`；worker另有逐文件原子备份 `backups/<timestamp>/manifest.txt`。备份含私有配置，只存本机。

失败恢复按所有权分离：Go installer在全局同步锁内负责worker/配置/worker plist的原子回滚；外层脚本仅恢复app/UI plist，绝不停止或覆盖worker。若worker升级成功、随后UI登录任务加载失败，保留已升级worker及其正常同步，明确报告“仅UI已回滚”，待同步空闲后重试；不承诺整包失败一定回到同一旧版本。初检后若有同步启动，Go installer拒绝busy，外层只恢复UI，不中断该同步。

回滚前先退出菜单栏，等待同步空闲，执行：

```bash
launchctl bootout "gui/$(id -u)/com.gitconverge.menubar" # 若已卸载可忽略该条失败
launchctl bootout "gui/$(id -u)/com.gitconverge.worker"
```

把当前 app 改名保留（不要覆盖其他版本），用 `ditto "$BACKUP/GitConverge.app" "$HOME/Applications/GitConverge.app"` 恢复旧 app；用 `cp -p` 恢复备份的 `gitconverge` 到 `~/.local/bin/gitconverge`、`com.gitconverge.worker.plist` 和（若原来存在）`com.gitconverge.menubar.plist` 到 `~/Library/LaunchAgents/`。首次从 CLI 版升级时原来没有 app/UI plist，只恢复旧 worker/plist，不创建 UI。按需核对版本和配置；不要默认恢复旧配置或 state 覆盖后续修改。恢复原先已加载的 plist：

```bash
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.gitconverge.worker.plist"
# 仅旧版本有UI时
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/com.gitconverge.menubar.plist"
```

卸载先退出菜单栏，然后运行发行包 `./uninstall.sh`。它卸载worker、停用两个登录任务，将app/UI plist移入恢复目录；保留配置、暂停偏好、状态、日志、备份、所有仓库以及 SSH 认证。不要手动删 Core/Skills。
