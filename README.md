# 近传 / NearTransfer

一个 Mac App，启动本机 H5 收发界面；其他设备在同一局域网扫码，直接用浏览器互传文字、图片和文件。无云服务、无账号、无需 Android App。

## 使用

1. 打开 `build/NearTransfer.app`。应用会自动打开默认浏览器，并提供控制窗口及菜单栏入口。
2. 手机与 Mac 连接同一 Wi-Fi，用手机相机扫描页面二维码，在浏览器打开链接。也可复制连接链接到另一台电脑。
3. 两端都可以发送文字、选择照片、选择多个文件或拖放文件；共享收件箱每 3 秒自动刷新。
4. 收到的文字可查看、复制；常见图片可预览；「保存」通过浏览器下载，保留原文件名和原始字节。
5. 保持 Mac App 运行。关闭网页不会停止服务；在控制窗口、菜单栏或应用菜单中退出近传会关闭服务。重启后重新扫码，历史内容保留。

只有本地 HTTP，**没有传输加密**。仅用于可信局域网；二维码及链接授予本次会话全部历史内容的读写访问，勿公开或转发给不信任的人。每次启动使用新的随机 256 位凭据。凭据从 URL fragment 读取后立即从地址栏移除，保存在当前标签页的 sessionStorage；不使用云端或外部静态资源。Mac 专属二维码接口使用独立管理凭据。会话结束时旧凭据失效。

如果网络有客户端隔离、Mac 防火墙拦截或地址变化，可能无法连接。先检查同一局域网和防火墙提示；多网卡时切换二维码上方的地址。更换网络后退出再打开。应用不修改 VPN、代理、路由、防火墙或系统证书。

## 限制与存储

- macOS 13+；构建脚本默认生成当前机器架构的开发版本。本机产物为 Apple Silicon arm64。
- 使用现代浏览器；移动端后台可能暂停刷新/上传。保持页面在前台。不保证断点续传。
- 每个文件最大 1 GiB；最多保留 1000 项；最多同时处理 4 个上传。超出限制会明确报错；磁盘不足也会拒绝发布未完成文件。
- 文本输入最多 100,000 个 UTF-16 代码单元。文字预览上限 1 MiB、图片预览上限 20 MiB；更大内容仍可保存。HEIC 等格式能传输，未承诺浏览器内预览。
- 原始文件和索引存于 `~/Library/Application Support/NearTransfer/Inbox/`，文件以随机 ID 保存，不覆盖同名上传。使用网页「保存」得到原文件名。不要在运行时手动编辑此目录。
- 如需归档整个收件箱：退出 App，将 `Inbox` 文件夹整体移动到备份位置，然后重新打开；新收件箱自动创建。旧目录的 `index.json` 保存名称与文件 ID 映射。当前无单项删除功能。
- 浏览器可能依据自身下载策略拦截某些下载；当前自动化环境中 Chrome 对图片和二进制下载均显示 `ERR_BLOCKED_BY_CLIENT`，Safari 已通过 PNG 和 1 MiB 二进制文件的最终保存及 SHA-256 核对；Chrome 保存流程仍未通过验收。不要关闭浏览器安全功能来绕过拦截。Mac 上的原始接收文件仍保留。
- 构建产物使用 ad-hoc 签名，仅验证本机完整性；没有 Developer ID 签名、公证或跨设备 Gatekeeper 安装验证。

## 开发

可作为独立仓库构建，也可保留在 REA 的 `lan-transfer/` 子目录中；不依赖 REA CLI/MCP 或 Lens。无第三方运行时/前端依赖；Go 标准库服务将 H5 静态资源嵌入二进制，Swift App 使用系统 AppKit 和 CoreImage 生成二维码。最终 App 不依赖机器上安装 Go、Node 或 Python。

需要 Xcode Command Line Tools / Swift 及 Go 1.24+：

```sh
bash scripts/build.sh
(cd engine && go test -race ./... && go vet ./...)
node --check engine/web/app.js
python3 scripts/check-lifecycle.py
bash scripts/package.sh
```

如果运行环境禁止监听端口，真实 HTTP 测试必须在允许本机 loopback 监听的环境运行，不能将跳过该测试记为通过。

引擎接口：`neartransfer-engine STORAGE_DIRECTORY QR_DIRECTORY`。启动后 stdout 仅写一行私有 bootstrap JSON（不要分享），stdin EOF 或 SIGTERM 触发有界退出。Mac 通过持有 stdin 管道拥有引擎生命周期；即使 App 异常退出，也不会遗留监听。存储目录旁的 advisory lock 防止多个进程并发修改同一索引。

验证范围见 [验收记录](docs/acceptance.md)。
