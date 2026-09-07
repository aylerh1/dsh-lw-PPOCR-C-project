# Composio 创建 GitHub 仓库规则

当需要使用 Composio 或其连接的 GitHub 账号创建公开/私有仓库时：
1. 从 `~/.gemini/config/mcp_config.json` 获取 Composio 端点及凭证；
2. 配合本地代理 `http://127.0.0.1:7897`，发送 Streamable HTTP MCP JSON-RPC 初始化请求；
3. 获取 `mcp-session-id` 后调用 `COMPOSIO_MULTI_EXECUTE_TOOL` 的 `GITHUB_CREATE_A_REPOSITORY_FOR_THE_AUTHENTICATED_USER` 工具创建仓库。
