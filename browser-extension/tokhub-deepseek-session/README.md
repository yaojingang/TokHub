# TokHub DeepSeek 登录态助手

该 Chrome 扩展在用户点击 TokHub 的“一键读取当前登录态”后，读取已打开 DeepSeek 网页中的 `localStorage.userToken.value`，并把 Token 直接传回当前 TokHub 页面。

## 安装

1. 解压下载的扩展包。
2. 在 Chrome 打开 `chrome://extensions`。
3. 开启“开发者模式”。
4. 点击“加载已解压的扩展程序”，选择解压后的目录。
5. 刷新 TokHub 页面。

## 使用

1. 在同一个 Chrome 用户配置中打开 `https://chat.deepseek.com` 并完成登录。
2. 确认 DeepSeek 网页可以正常发起对话。
3. 回到 TokHub 的 DeepSeek 网页账号连接页。
4. 点击“一键读取当前登录态”。

## 权限与数据范围

- DeepSeek 访问范围固定为 `https://chat.deepseek.com/*`。
- TokHub 默认支持 `tokhub.me`、`www.tokhub.me`、本地开发端口 `5173`、`8080` 和 `28125`。
- 扩展只读取 `userToken.value`。
- 扩展不读取 Cookie、密码或其他 Local Storage 数据。
- Token 仅在本次点击产生的内存消息中传递，扩展不持久化保存。
- TokHub 服务端会验证 Token，并在验证通过后使用 AES-256-GCM 加密保存。
