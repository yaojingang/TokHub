# TokHub AI 登录助手

该 Chrome 扩展支持两种用户主动发起的登录识别：

- ChatGPT 登录完成后，识别浏览器停留的 `http://localhost:1455/auth/callback` 单次 OAuth 回调地址。
- DeepSeek 登录完成后，读取已打开网页中的 `localStorage.userToken.value`。

识别结果直接传回当前 TokHub 页面，扩展不持久化保存。

## 安装

1. 解压下载的扩展包。
2. 在 Chrome 打开 `chrome://extensions`。
3. 开启“开发者模式”。
4. 点击“加载已解压的扩展程序”，选择解压后的目录。
5. 刷新 TokHub 页面。

## 使用

### ChatGPT

1. 在 TokHub 选择“登录 ChatGPT”并打开授权窗口。
2. 完成登录，浏览器会停留在 `localhost:1455` 回调页；页面显示无法访问也不影响识别。
3. 回到 TokHub，点击“一键识别授权结果”。

### DeepSeek

1. 在同一个 Chrome 用户配置中打开 `https://chat.deepseek.com` 并完成登录。
2. 确认 DeepSeek 网页可以正常发起对话。
3. 回到 TokHub 的 DeepSeek 网页账号连接页。
4. 点击“一键读取当前登录态”。

## 权限与数据范围

- DeepSeek 访问范围固定为 `https://chat.deepseek.com/*`。
- ChatGPT 只读取 `http://localhost:1455/auth/callback` 的 `code` 与 `state` 回调参数，并只返回当前 TokHub 授权事务对应的页面。
- TokHub 默认支持 `tokhub.me`、`www.tokhub.me`、本地开发端口 `5173`、`8080` 和 `28125`。
- 扩展不会访问 ChatGPT Cookie、Local Storage 或网页 Token。
- DeepSeek 只读取 `userToken.value`。
- 扩展不读取 Cookie、密码或其他 Local Storage 数据。
- 识别结果仅在本次点击产生的内存消息中传递。
- TokHub 服务端会校验 OAuth state 或 DeepSeek Token，并在验证通过后使用 AES-256-GCM 加密保存最终凭证。
