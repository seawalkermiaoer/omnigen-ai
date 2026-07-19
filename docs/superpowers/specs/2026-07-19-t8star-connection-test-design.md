# t8star 连接测试

- 日期：2026-07-19
- 分支：`rewrite-react-go`
- 前置：设置页三卡片结构已完成（`endpoint` 已拆分为 `dashscope_endpoint` / `t8star_endpoint`）

## 目标

设置页的 t8star 卡片加一个「测试连接」按钮，和阿里云百炼卡片上那个对齐，让管理员填完 Key 后能当场确认凭证是否可用，而不是等到第一次生成失败才发现。

## 核心问题：t8star 没有便宜的探测路径

这不是实现细节，是决定方案形状的约束。

DashScope 的测试连接调的是 `compatible-mode/v1/chat/completions` 上的一个文本模型（`qwen3.7-plus`），发一个极短的请求，成本是几厘钱级别，点多少次都无所谓。

**t8star 只有一个上游端点**：`POST {base}/v1/chat/completions`，模型固定 `gpt-image-2`。这个调用的语义就是生成图片。它没有 `/models`、没有 `/ping`、没有任何只验凭证不干活的接口。

所以照搬 DashScope 的做法会得到一个「每点一次就真出一张图、真产生一次费用」的按钮。等算力配额功能上线后，它还会真扣一次用户额度——一个「测试」动作扣掉真实配额，是明显说不通的。

## 方案

**发一个最小请求，按 HTTP 状态码分类，而不是按是否拿到图片分类。**

- 请求体：`{model: "gpt-image-2", stream: false, messages: [{role: "user", content: ""}]}`——空 prompt
- **401 / 403** → 凭证无效，报 `UPSTREAM_AUTH_FAILED`，文案指向「请检查 t8star API Key」
- **其他任何响应（含 4xx 业务错误、含成功出图）** → 凭证有效。测试的目标是验证凭证，不是验证能出图。
- 网络层失败 → `UPSTREAM_FAILED`，文案指向「无法连接到 t8star，请检查接入地址」

绝大多数 API 在生成之前就会校验鉴权，所以空 prompt 大概率在扣费前就被拒。但**这一点无法在没有真实 Key 的情况下验证**——本项目当前没有可用的 t8star 凭证。

因此界面上必须诚实：t8star 卡片的测试按钮下方注明「t8star 没有独立的鉴权探测接口，本次测试会向上游发起一次真实请求，可能产生少量费用」。百炼卡片的按钮不需要这句话，因为它确实是免费的。

**放弃的方案：**

- *不提供测试按钮*——那样管理员只能靠第一次生成失败来判断 Key 对不对，而生成失败的原因很多（参数、地域、模型限制），凭证问题会混在里面难以定位。
- *真生成一张小图并展示*——把"测试"变成"生成"，语义错位，且必然计费。
- *点击前弹确认框*——多一次交互换一个大概率不会发生的费用，不值得；一行说明文字足够。

## 实现

现在的 `SettingService.TestConnection(ctx)` 硬编码只测 DashScope（`newDashscopeConnectionTester`），需要按 provider 分派。

**接口**：`POST /api/settings/test` 增加请求体 `{provider: "dashscope" | "t8star"}`。缺省为 `dashscope`，保持与现有前端调用的兼容。非法值返回 `VALIDATION_FAILED`。

**服务层**：`TestConnection(ctx, provider)` 按 provider 选择 tester 与配置来源：

| provider | API Key | Endpoint | 其他 |
|---|---|---|---|
| `dashscope` | `dashscope_api_key` | `dashscope_endpoint` | `region`、`workspace_id` |
| `t8star` | `t8star_api_key` | `t8star_endpoint` | 无——t8star 没有地域和 workspace 概念 |

t8star 的 tester 复用 `provider/t8star` 的 `Client`，不新写一份 HTTP 调用——协议解析已经在那里且有真实报文夹具覆盖。

**未配置对应 Key 时**返回 `SETTING_INCOMPLETE`，且**不发出任何上游请求**。这一点要有测试：用假 provider 断言调用次数为 0。

**前端**：t8star 卡片加按钮与结果区，复用百炼卡片已有的成功/失败展示组件，并带上前述费用说明。

## 测试要点

- `provider=t8star` 走 t8star tester，`provider=dashscope` 走 DashScope tester，**互不串**（这是 `endpoint` 拆分那个 bug 的同类风险：一个共用字段喂给两个 provider）
- 缺省不传 provider 时等价于 `dashscope`
- 非法 provider 值返回 `VALIDATION_FAILED`
- t8star Key 未配置 → `SETTING_INCOMPLETE`，且上游零调用
- 上游返回 401/403 → `UPSTREAM_AUTH_FAILED`
- 上游返回 4xx 业务错误 → **判定为成功**（凭证有效）。这条容易写反，必须显式测
- 上游连接失败 → `UPSTREAM_FAILED`
- 前端：两个按钮各自触发正确的 provider，结果互不覆盖

## 明确不做

- 不为 t8star 探测可用模型列表——它只有 `gpt-image-2`，目录里已写死。
- 不缓存测试结果。凭证随时可能被上游吊销，缓存会给出过期的安全感。
- 不做定时健康检查。

## 一个待确认项

「空 prompt 请求是否在扣费前被拒」无法在没有真实 t8star 凭证的情况下验证。拿到 Key 后应实测一次：发一个空 prompt 请求，观察上游是返回鉴权错误、参数错误，还是真的生成并计费。若结果是后者，把界面说明从「可能产生少量费用」改为明确的「会产生一次图片生成费用」，并考虑改回点击确认。
