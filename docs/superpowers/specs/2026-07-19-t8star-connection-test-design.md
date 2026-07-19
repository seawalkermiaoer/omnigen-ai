# t8star 连接测试

- 日期：2026-07-19
- 分支：`rewrite-react-go`
- 前置：设置页三卡片结构已完成（`endpoint` 已拆分为 `dashscope_endpoint` / `t8star_endpoint`）

## 目标

设置页的 t8star 卡片加一个「测试连接」按钮，和阿里云百炼卡片上那个对齐，让管理员填完 Key 后能当场确认凭证是否可用，而不是等到第一次生成失败才发现。

## 核心问题：t8star 没有独立的 `/ping` 端点

**t8star 只有一个上游端点**：`POST {base}/v1/chat/completions`。它没有 `/models`、没有 `/ping`。

**2026-07-19 更新：下面这一节最初的结论错了，已用真实 Key 实测推翻，见「实测结果与修正」。** 保留原文是为了如实记录当时的推理，不是因为它还成立。

<details>
<summary>原始方案（已被实测推翻，仅存档）</summary>

DashScope 的测试连接调的是 `compatible-mode/v1/chat/completions` 上的一个文本模型（`qwen3.7-plus`），发一个极短的请求，成本是几厘钱级别，点多少次都无所谓。

t8star 这个调用的语义就是生成图片，没有任何只验凭证不干活的接口。所以照搬 DashScope 的做法会得到一个「每点一次就真出一张图、真产生一次费用」的按钮。等算力配额功能上线后，它还会真扣一次用户额度——一个「测试」动作扣掉真实配额，是明显说不通的。

**发一个最小请求，按 HTTP 状态码分类，而不是按是否拿到图片分类。**

- 请求体：`{model: "gpt-image-2", stream: false, messages: [{role: "user", content: ""}]}`——空 prompt
- **401 / 403** → 凭证无效，报 `UPSTREAM_AUTH_FAILED`
- **其他任何响应（含 4xx 业务错误、含成功出图）** → 凭证有效
- 网络层失败 → `UPSTREAM_FAILED`

绝大多数 API 在生成之前就会校验鉴权，所以空 prompt **假设**大概率在扣费前就被拒——但这条假设当时无法在没有真实 Key 的情况下验证。

因此界面上当时要求诚实注明：「t8star 没有独立的鉴权探测接口，本次测试会向上游发起一次真实请求，可能产生少量费用」。

</details>

## 实测结果与修正（2026-07-19，用真实 Key 对真实上游测的）

上面「空 prompt 大概率在扣费前就被拒」这条假设是**错的**。实测如下：

**用真实模型名 `gpt-image-2` + 空 prompt**（原方案）：

```
HTTP 200，耗时 29.2 秒
{"model":"gpt-image-2-pro","choices":[{"message":{"content":"![image](https://webstatic.aiproxy.vip/output/....png)"}}],
 "usage":{"prompt_tokens":6,"completion_tokens":1149,"total_tokens":1155}}
```

空 prompt **没有**被提前拒绝——上游真的生成了一张图，扣了 1155 tokens。而 `connectionTestTimeout`（10s）远早于 29.2s 触发，导致这个按钮上线以来其实**每次点击都必然超时失败**（`UPSTREAM_FAILED`），**同时仍然真实扣费**——比原方案设想的「小概率产生费用」严重得多：是「每次必然产生费用，还必然报错」。

**改用一个刻意不存在的哨兵模型名**（新方案）：

```
正确 Key + "definitely-not-a-real-model-xyz"（探测阶段试验用的占位名，
生产代码里用的是 __omnigen_credential_probe__）：
  HTTP 503，耗时 0.7 秒
  {"error":{"code":"invalid_request","message":"所有分组对于模型 ... 无可用渠道，请检查模型是否存在或联系管理员","type":"new_api_error"}}

错误 Key + 同一哨兵模型名：
  HTTP 401，耗时 0.2 秒
  {"error":{"code":"invalid_request","message":"令牌不合法","type":"new_api_error"}}
```

**鉴权先于模型路由完成**。用一个必然不存在的模型名探测，得到的是一个真正只验证凭证、不做其他任何事的探测：亚秒级、免费、不出图、不产生 usage。

## 方案（修正后）

**发一个最小请求，模型名固定为哨兵值，按 HTTP 状态码分类，而不是按是否拿到图片分类。**

- 请求体：`{model: "__omnigen_credential_probe__", stream: false, messages: [{role: "user", content: ""}]}`——模型名是刻意不存在的哨兵值，不是 `gpt-image-2`
- **401 / 403** → 凭证无效，报 `UPSTREAM_AUTH_FAILED`，文案指向「请检查 t8star API Key」
- **其他任何响应（预期是 4xx/5xx 的"模型不存在/无可用渠道"业务错误）** → 凭证有效。测试的目标是验证凭证，不是验证能出图。
- 网络层失败 → `UPSTREAM_FAILED`，文案指向「无法连接到 t8star，请检查接入地址」

分类规则本身（401/403 → 认证失败；其余响应 → 凭证有效；无响应 → 连接失败）没有变，变的只是探测请求本身——从「真实模型 + 空 prompt」换成「哨兵模型名」。

界面上不再需要费用警告：探测不生成图片、不计费。t8star 卡片测试按钮下方的说明改为如实的中性文案（「测试连接使用一个占位模型名探测凭证，不会生成图片，也不会产生费用」），不再用 warning 语气。

**放弃的方案：**

- *不提供测试按钮*——那样管理员只能靠第一次生成失败来判断 Key 对不对，而生成失败的原因很多（参数、地域、模型限制），凭证问题会混在里面难以定位。
- *真生成一张小图并展示*——把"测试"变成"生成"，语义错位，且必然计费。
- *点击前弹确认框*——哨兵模型方案下已经没有费用风险，不需要这层交互。
- *继续用真实模型名 + 空 prompt，只是把超时调大到 30s+*——依然每次点击都真实计费，只是不再报错；没有解决问题的根源，纯粹是把「报错」这一半症状盖住。

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

**前端**：t8star 卡片加按钮与结果区，复用百炼卡片已有的成功/失败展示组件，并带上如实的（非警告语气的）探测说明。

## 测试要点

- `provider=t8star` 走 t8star tester，`provider=dashscope` 走 DashScope tester，**互不串**（这是 `endpoint` 拆分那个 bug 的同类风险：一个共用字段喂给两个 provider）
- 缺省不传 provider 时等价于 `dashscope`
- 非法 provider 值返回 `VALIDATION_FAILED`
- t8star Key 未配置 → `SETTING_INCOMPLETE`，且上游零调用
- 探测请求发的是哨兵模型名，不是 `gpt-image-2`——断言实际发出的请求体
- 上游返回 401/403 → `UPSTREAM_AUTH_FAILED`
- 上游返回 503 业务错误（真实上游对合法 Key + 哨兵模型名的实际响应）→ **判定为成功**（凭证有效）。这条容易写反，必须显式测
- 上游连接失败 → `UPSTREAM_FAILED`
- 前端：两个按钮各自触发正确的 provider，结果互不覆盖；t8star 说明文案是如实内容，不再是费用警告

## 明确不做

- 不为 t8star 探测可用模型列表——生成场景只用 `gpt-image-2`，目录里已写死；测试连接用的哨兵模型名只服务于凭证探测，与「支持哪些模型」无关。
- 不缓存测试结果。凭证随时可能被上游吊销，缓存会给出过期的安全感。
- 不做定时健康检查。

## 待确认项（已解决，2026-07-19）

原文提出的待确认项是：「空 prompt 请求是否在扣费前被拒」无法在没有真实 t8star 凭证的情况下验证，拿到 Key 后需要实测。

**现已用真实 Key 实测，结论与原假设相反**：空 prompt 请求**没有**在扣费前被拒——用真实模型名 `gpt-image-2` 发空 prompt，上游花 29.2 秒真实生成了一张图并扣费 1155 tokens；`connectionTestTimeout`（10s）会先于此触发，导致按钮必然超时失败但仍然真实扣费。

这条负面结果没有让原方案「改进」（比如调大超时、保留费用警告），而是直接推翻了它的前提——**改成不用真实模型名探测**：用一个刻意不存在的哨兵模型名（`__omnigen_credential_probe__`），因为 t8star 的鉴权发生在模型路由之前，一个不存在的模型名同样能验证凭证，而且是免费、亚秒级、不出图的。详见上面「实测结果与修正」一节的完整数据。

界面上的费用警告文案已随之移除，换成如实的中性说明。
