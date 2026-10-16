# 三方登录（OAuth2）接入与验证指南

本文回答两件事：**去哪里拿真实的三方凭据**，以及**拿到后怎么验证整条登录链路是通的**。

提供方：**GitHub / Google / 微信（开放平台网站应用扫码）**。

---

## 0. 先看清代码现状（避免踩已知坑）

| 项 | 现状 | 影响 |
|---|---|---|
| 提供方 | GitHub / Google / 微信 | 三方均已注册路由 |
| 凭据为空时 | `Enabled()=false`，`/oauth/providers` 返回 `enabled:false` | 前端应据此隐藏入口，不是报错 |
| 回调路径 | `GET/POST /user/api/v1/oauth/{provider}/callback` | 必须匿名放行（已配） |
| GitHub | 无 id_token，靠 `/user` 端点 | 可直接端到端验证 |
| Google | 走 OIDC，调 `tokeninfo` 校验 aud/iss | 可直接端到端验证 |
| 微信 | 无 id_token，靠 `access_token + openid` | 实现完备，**待真实凭据验证** |
| 单元测试 | 12 项，覆盖 URL 生成 / errcode / 空标识 | `go test ./internal/oauth2/` |

配置位置：`devops-user/config/devops-user-dev.yaml` 的 `oauth:` 段（dev 环境）。
生产值**禁止提交入库**，由 Nacos 或密钥管理注入。

---

## 1. GitHub（最容易，建议先用它打通链路）

### 1.1 申请凭据

1. 登录 GitHub → 右上角头像 → **Settings**
2. 左侧最下developer settings → **OAuth Apps** → **New OAuth App**
   （或直接访问 `https://github.com/settings/developers`）
3. 填写：

| 字段 | 本地开发填什么 |
|---|---|
| Application name | `devops-user-dev` |
| Homepage URL | `http://localhost:10000` |
| Authorization callback URL | `http://localhost:10000/user/api/v1/oauth/github/callback` |

4. **Register application** → 页面直接给出 **Client ID**
5. 右侧 **Generate a new client secret** → **Client secret 只显示这一次**，立刻复制

### 1.2 关键规则（会导致 `redirect_uri mismatch`）

- 回调地址**逐字匹配**：协议、域名、端口、路径、尾斜杠全都要一致。
- 建议**只填一个**回调地址，避免误配。若必须填多个，用 `http://127.0.0.1/...` 之类的回环地址而非 `localhost`（RFC 建议用回环字面量）。
- 多环境（dev/staging/prod）要**各建一个 OAuth App**，因为一个 App 只有一个 callback URL 字段（可用请求参数 `redirect_uri` 覆盖端口，但路径必须是登记路径的子目录）。

### 1.3 写入配置

```yaml
oauth:
  github:
    client_id: "Ov23lixxxxxxxxxxxx"
    client_secret: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
    redirect_url: "http://localhost:10000/user/api/v1/oauth/github/callback"
    auto_register: true      # 打通链路时先开，验证完可改回 false
    nickname_prefix: "GH:"
```

> `auto_register: true` 时三方首次登录会自动建本地账号（口令是随机 32 字节强随机，用户不知道）。
> 保持默认 `false` 则未绑定账号会返回「该三方账号尚未绑定，请先登录后绑定」。

---

## 2. Google

### 2.1 申请凭据

1. 打开 `https://console.cloud.google.com/apis/credentials`
2. 顶部选/建一个项目
3. **Create credentials → OAuth client ID**
4. Application type 选 **Web application**
   （有后端换token 就必须选 Web，纯前端隐式流程才选别的）
5. 填写：

| 字段 | 本地开发填什么 |
|---|---|
| Name | `devops-user-dev` |
| Authorized redirect URIs | `http://localhost:10000/user/api/v1/oauth/google/callback` |
| Authorized JavaScript origins | 本地可留空（服务端重定向流程用不到） |

6. 创建后弹窗给出 **Client ID** 和 **Client secret**（secret 也只显示一次，可下载 `client_secret_*.json`）

### 2.2 Google 的两个额外坑

- **端口必须明确写**：`http://localhost:8080` 与 `http://localhost:10000` 是两条不同记录，本项目固定用 **10000**。
- **测试用户**：未通过 Google 验证的应用只有项目成员/测试用户能登录。需去 **OAuth 同意屏幕 → 测试用户** 添加自己的 Google 账号，否则会看到「Access blocked」。

### 2.3 写入配置

```yaml
  google:
    client_id: "1234567890-abc123def456.apps.googleusercontent.com"
    client_secret: "GOCSPX-xxxxxxxxxxxx"
    redirect_url: "http://localhost:10000/user/api/v1/oauth/google/callback"
    scopes:
      - openid
      - https://www.googleapis.com/auth/userinfo.email
      - https://www.googleapis.com/auth/userinfo.profile
    auto_register: true
    nickname_prefix: "Google:"
```

---

## 3. 微信（开放平台网站应用扫码）

### 3.1 前置条件（比 GitHub/Google 麻烦，先确认有）

| 条件 | 说明 |
|---|---|
| **企业主体 + 300 元认证** | 微信开放平台网站应用**个人主体基本申请不下来**，需营业执照等资质 |
| **已ICP 备案的域名** | 备案主体须与开放平台主体一致（或能提供关联证明），否则审核直接打回 |
| **开放平台账号** | `https://open.weixin.qq.com`；注册用的邮箱不能绑过任何公众号/小程序/支付 |

> **没有企业资质就别硬接。** 若只是想要「微信快捷登录」体验而拿不到资质，
> 替代方案是**微信小程序码登录**（个人实名即可、0 元）：网页弹小程序码，
> 用户扫进小程序确认后回调网页登录。代价是需要一个小程序 + 自建中转服务。

### 3.2 申请步骤

1. 登录 `https://open.weixin.qq.com` → 完成**开发者资质认证**（300 元，1–3 个工作日）
2. **管理中心 → 网站应用 → 创建网站应用**
3. 填写材料：

| 字段 | 填什么 |
|---|---|
| 应用名称 | `devops-user` |
| 网站域名 | 你的**一级域名**，如 `yourdomain.com` |
| **授权回调域** | 只填**域名**，如 `devops.local` |

4. 审核通过（1–3 天）→ 应用详情页拿到 **AppID** 和 **AppSecret**

### 3.3 授权回调域的四个坑（最常见的失败原因）

- **不能带协议**：`https://example.com/callback` 是错的，正确填 `example.com`
- **不能带端口**：`example.com:8443` 的端口部分**完全不被识别**，只填 `example.com`
- **不能带路径**：只填域名部分
- **不能是 IP 或 localhost**：本地调试须改 hosts

本地调试二选一：

```bash
# 方案 A：改 hosts（hosts 文件加一行）
#   127.0.0.1  devops.local
# 然后配置 redirect_url = http://devops.local:10000/user/api/v1/oauth/wechat/callback

# 方案 B：Nginx 反代一个测试域名到本地
```

### 3.4 ⚠️ 三套 AppID 互不相通（头号大坑）

微信生态里有**三套完全独立**的 AppID，接口、授权流程、token 全都不一样：

| 体系 | 入口 | 本项目用哪个 |
|---|---|---|
| **开放平台网站应用** | `open.weixin.qq.com` | ✅ **就是这个** |
| 公众号（网页授权） | `mp.weixin.qq.com` | ❌ 不是它 |
| 小程序 | `mp.weixin.qq.com` | ❌ 不是它 |

拿公众号/小程序的 AppID 去调 `/sns/oauth2/access_token`，必然报 `40013 invalid appid`。

### 3.5 写入配置

```yaml
  wechat:
    client_id: "wx1234567890abcdef"     # 网站应用的 AppID
    client_secret: "xxxxxxxxxxxxxxxx"   # AppSecret，泄露须立即在后台重置
    redirect_url: "http://devops.local:10000/user/api/v1/oauth/wechat/callback"
    # ⚠️ scopes 配了也会被忽略：网站应用只允许 snsapi_login
    auto_register: true                # 打通链路时先开
    nickname_prefix: "微信:"
```

### 3.6 实现上必须注意的四个特殊点

微信授权端点**不是标准 OAuth2 端点**，`WeChatProvider` 手工拼 URL：

```
https://open.weixin.qq.com/connect/qrconnect
  ?appid=APPID
  &redirect_uri=<urlEncode后的回调地址>   ← 只编码一层
  &response_type=code
  &scope=snsapi_login← 唯一合法值
  &state=STATE
  #wechat_redirect                        ← 缺了必报「该链接无法访问」
```

| 坑 | 后果 |
|---|---|
| 缺 `#wechat_redirect` | 微信提示「该链接无法访问」 |
| 用 `url.Values.Encode()` | `%` 被编成 `%25` 双重编码，回调地址解析错误 |
| `scope` 填别的值 | 直接被拒 |
| **只判 HTTP 状态码** | ⚠️ 微信**出错也返回 200**，错误在 body 的 `errcode` 里 |

最后一条是安全关键：`{"errcode":40029,"errmsg":"invalid code"}` 的 HTTP 状态是 200。
若只判状态码，会把失败当成功 → 写入空绑定键 → 所有此类账号互相错绑。
代码里对 `errcode`（40029 invalid code / 40013 invalid appid / 40125 invalid appsecret /
40003 invalid openid）逐个显式判失败，且 `openid` 与 `unionid` 都为空时拒绝登录。

### 3.7 绑定键与已知限制

绑定键取 **`unionid` 优先**（同一开放平台账号下跨应用唯一），缺失时退回 `openid`；
两者皆空则拒绝登录。

⚠️ 两条产品侧限制：
- **微信不返回邮箱，也不允许索取** → 纯微信用户的 `email` 恒为空，
  「邮箱找回密码」对其不可用，需另设绑定手机号/邮箱流程
- **头像 URL 在用户改头像后即失效** → 当前仅作展示快照，重要场景须转存图片

---

## 4. 验证登录流程是否正常

### 4.1 启动前置依赖

```bash
# MySQL / Redis / Nacos（用项目自带的 compose）
docker compose -f docker-compose-dev.yml up -d
```

### 4.2 启动服务

```bash
cd devops-user
APP_ENV=dev go run ./cmd/devops-user -f config/bootstrap.yaml
```

> 改完代码**必须确认重新编译成功再重启**，否则会用旧二进制验证出错误结论。

### 4.3 第一步：确认凭据已被识别

```bash
curl -s http://127.0.0.1:10000/user/api/v1/oauth/providers | jq
```

期望：`github.enabled = true`，且 `authorize_url` 里带 `client_id` 和一串随机 `state`：

```json
{
  "code": 200, "msg": "ok",
  "data": [
    { "name": "github", "enabled": true,
      "authorize_url": "https://github.com/login/oauth/authorize?client_id=Ov23...&redirect_uri=...&state=8Xk2..." },
    { "name": "google", "enabled": false },
    { "name": "wechat", "enabled": false }
  ]
}
```

- `enabled:false` → 三项里有空值，回去检查 `client_id` / `client_secret` / `redirect_url`
- 连 `providers` 都 404/连不上 → 服务没起来，或前缀不是 `/user`

### 4.4 第二步：浏览器完成授权

把 `authorize_url` 粘到浏览器打开并授权，浏览器会跳到

```
http://localhost:10000/user/api/v1/oauth/github/callback?code=xxx&state=yyy
```

微信则跳到（用 §3.5 配的域名）：

```
http://devops.local:10000/user/api/v1/oauth/wechat/callback?code=xxx&state=yyy
```

回调返回 JSON 即成功：

```json
{
  "code": 200, "msg": "ok",
  "data": {
    "token_pair": { "access_token": "eyJ...", "refresh_token": "...", "expires_in": 7200 },
    "bound": false, "user_id": 1900000000000001, "nickname": "GH:你的昵称"
  }
}
```

### 4.5 第三步：核对数据落库

```sql
SELECT id, username, nickname, email, disabled FROM t_user ORDER BY create_time DESC LIMIT 5;
SELECT id, user_id, provider, provider_user_id, nickname FROM t_user_oauth;
```

- `provider_user_id` 应该是三方**稳定 ID**（GitHub 数字 ID、Google `sub`、微信 `unionid`），**不是** username/email/nickname（可改、可换、可重复，会导致错绑）
- 微信用户的 `email` **必然为 NULL**（微信不返回邮箱），这不是 bug
- Redis 里只应看到 refresh 的**哈希**，看不到明文：
  ```bash
  redis-cli -a 123456 -n 1 --scan --pattern 'refresh*'
  ```

### 4.6 第四步：用 access token 访问受保护接口

```bash
curl -s http://127.0.0.1:10000/user/api/v1/profile \
  -H "Authorization: Bearer <access_token>" | jq
```

能返回用户资料 → 说明三方身份已正确换成本地令牌。

### 4.7 不接真实凭据也能跑的验证

微信需要企业资质才能拿到真实 AppID，此时可先跑单元测试确认实现无误：

```bash
cd devops-user && go test ./internal/oauth2/ -v
```

12 项测试覆盖：URL 形态（端点 / 编码 / scope / `#wechat_redirect`）、
正常换token 拉用户、unionid 缺失退回 openid、**标识全空必须拒绝**、
`errcode` 40029/40013/40125/40003 逐个失败、头像尺寸段规整等。

---

## 5. 常见失败原因速查

| 现象 / 错误码 | 原因 | 处理 |
|---|---|---|
| `/oauth/providers` 里 `enabled:false` | 三项配置有空的 | 补齐 `client_id`/`client_secret`/`redirect_url` |
| GitHub `redirect_uri_mismatch` | 回调地址与后台不一致（含尾斜杠、http/https、端口） | 逐字对齐；GitHub 的 `redirect_uri` 路径须是登记路径的子目录 |
| Google `Access blocked: 未验证的应用` | 未过验证的应用只允许测试用户 | OAuth 同意屏幕 → 测试用户 加入你的账号 |
| Google `Error 400: redirect_uri_mismatch` | 端口或路径不符 | 确认写的是 `:10000` |
| 微信「该链接无法访问」 | 授权 URL 缺 `#wechat_redirect` | 代码已自动追加；若自行拼 URL 需注意 |
| 微信 `errcode=40013 invalid appid` | 用了公众号/小程序的 AppID | 必须是**开放平台网站应用**的 AppID，见 §3.4 |
| 微信 `errcode=40125 invalid appsecret` | AppSecret 填错或已重置 | 后台复制当前值 |
| 微信 `errcode=40029 invalid code` | code 超时（10 分钟）或已被用过 | 重新发起授权 |
| 微信 `errcode=40003 invalid openid` | access_token 超时或 openid 不匹配 | 重新走授权流程 |
| 微信回调未跳回 | 授权回调域填了协议/端口/路径 | 只填域名，见 §3.3 |
| `state 校验失败，请重新发起登录` | state 过期（10m TTL）或已用过 | 重新调 `providers` 取新的 authorize_url；**每个 state 只能用一次**，重放必然失败 |
| `state 与提供方不匹配` | 用 A 的 state 走 B 的回调 | 正常防护，别绕过 |
| `三方授权失败 / 你已取消授权` | 用户点了取消 | 属预期路径 |
| `该三方账号尚未绑定，请先登录后绑定` | `auto_register:false` 且该三方账号首次出现 | 先注册/登录本地账号，再调 `POST /oauth/bind/start` 绑定 |
| `该三方账号已绑定其他用户` | 同一三方账号绑到了别的本地用户 | 设计如此；换账号或先解绑 |

---

## 6. 一键回归脚本

```bash
#!/usr/bin/env bash
set -euo pipefail
BASE=http://127.0.0.1:10000

echo "== 1. 提供方状态 =="
curl -s "$BASE/user/api/v1/oauth/providers" | jq '.data[] | {name, enabled}'

echo "== 2. 取 github 授权地址（有效期 10 分钟）=="
AUTH_URL=$(curl -s "$BASE/user/api/v1/oauth/providers" \
  | jq -r '.data[] | select(.name=="github" and .enabled==true) | .authorize_url')
[ -n "$AUTH_URL" ] && [ "$AUTH_URL" != null ] || { echo "github 未启用"; exit 1; }
echo "$AUTH_URL"

echo "== 3. 请在浏览器打开上面的地址完成授权，然后粘贴回调 URL =="
read -r CB
CODE=$(sed -n 's/.*[?&]code=\([^&]*\).*/\1/p' <<<"$CB")
STATE=$(sed -n 's/.*[?&]state=\([^&]*\).*/\1/p' <<<"$CB")

echo "== 4. 重放同一个 state（必须失败，证明 state 一次性）=="
curl -s "$BASE/user/api/v1/oauth/github/callback?code=$CODE&state=$STATE" | jq -c '{code,msg}'

echo "== 5. 重新走一遍拿真实结果 =="
# ... 同上，重新取 authorize_url 并授权，然后：
# curl -s "$BASE/user/api/v1/oauth/github/callback?code=$CODE2&state=$STATE2" | jq
```

第 4 步是重点：用同一个 `code`+`state` 再打一次，必须返回 `state 校验失败`。
如果第二次还能成功，说明 state 没有一次性消费，属于 CSRF 漏洞。