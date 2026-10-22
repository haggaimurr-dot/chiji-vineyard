# 云托管部署手册

身份与权限新增登录限流、细粒度操作权限：已有数据库先执行 `backend/migrations/20261005_identity.sql`，再部署后端与后台；经代理部署时配置 `LOGIN_TRUSTED_PROXY_CIDRS`。详见 [身份与权限说明](IDENTITY_PERMISSIONS.md)。

合作线索负责人、跟进历史和提醒：先执行 `backend/schema.sql` 中 `partner_lead_history` 建表语句，再部署新版后端及后台。旧线索 JSON 无需修改，旧跟进备注会保留。当前提醒为后台站内提醒，不需要外部消息服务配置，详见 [线索跟进说明](LEAD_WORKFLOW.md)。

配置版本与回滚、操作日志：部署新版服务前执行 `backend/schema.sql` 中的 `config_versions` 建表语句；详细操作、权限、回滚冲突及验收步骤见 [配置历史说明](CONFIG_HISTORY.md)。新接口依赖该表，旧配置在首次变更时自动保留基线快照。

批量发货、订单导出及快递100电子面单的新增表、服务端账号配置、状态核实与实单验收见 [订单运营操作手册](ORDER_OPERATIONS.md)。电子面单须单独开通服务及额度，沿用企业 KEY 并新增 SECRET、寄件人及网点/月结账号；仅有实时轨迹查询凭据不能创建面单。

种植内容新增功能的部署要求：先执行 `backend/schema.sql` 中 `article_versions`、`article_schedules`、`article_reads` 建表语句，再部署后端、后台和小程序。定时任务每30秒处理一次，内容请求也会补处理；云托管缩容到零时下一次请求补发。详见 [定时发布、内容版本与阅读统计](CONTENT_FEATURES.md)。

## 准备资源

先取得已认证的微信小程序 AppID、已绑定的微信支付商户号，以及适用的商品类目。分别创建开发、验收、生产云开发环境；环境之间不得共用 MySQL、支付回调地址或管理员账号。Go 服务部署到云托管，PC 后台可部署到静态网站托管。

在每个环境创建独立 MySQL 数据库并执行 `backend/schema.sql`。`MYSQL_DSN` 由 Go 服务读取，使用 `.env.example` 中的 `parseTime=true&loc=UTC&charset=utf8mb4&time_zone=%27%2B00%3A00%27`，保证会话时间为 UTC；数据库只允许后端访问，不应向小程序或浏览器开放。当前迁移针对新项目空库；若旧云数据库已经有正式订单，须先设计并验证迁移脚本，不能直接切库。

## 部署 Go 容器

以 `backend/` 为 Docker 构建目录部署云托管服务，服务名可设为 `chiji-api`。容器从环境变量 `PORT` 监听（默认 8080）；健康检查为 `GET /healthz`。云托管与 MySQL 要处于可互通网络。

在云托管的密钥/环境配置中设置根目录 `.env.example` 列出的变量。至少需要 `MYSQL_DSN`、`TOKEN_SECRET`、`WECHAT_APP_ID`、`WECHAT_APP_SECRET`；支付环境另需商户证书私钥、微信支付公钥、公钥 ID、API v3 密钥和回调地址。私钥可用真实换行，也可用 `\n` 表示换行。请勿把密钥放入前端构建或仓库。

为服务绑定公网 HTTPS 域名，`PUBLIC_BASE_URL` 指向该域名，用于后台上传图片的可访问地址。`ADMIN_ALLOWED_ORIGIN` 填 PC 后台实际域名。公网提供：

- `POST /api`：PC 后台接口；小程序通过云托管内的 `wx.cloud.callContainer` 调用同一路径。
- `POST /wechat-pay-notify`：支付与退款回调；必须可被微信支付服务器访问。
- `GET /media/{id}`：后台上传图片的公开访问地址。

小程序在 `miniprogram/app.ts` 设置 `demoMode: false`、`cloudEnv` 和 `cloudService`，`project.config.json` 设置真实 AppID。PC 后台构建时设置 `VITE_USE_MOCK=false` 与 `VITE_API_BASE_URL=https://你的域名/api`。

## 管理员与运营数据

账号创建、启停和密码重置可在「运营配置—后台账号」完成。密码为 10–128 位；重置会使该账号全部旧管理员会话失效，自重置后立即重新登录。新版令牌绑定密码凭据，升级前的管理员令牌需重新登录，无需为此新增数据库字段。保存预览与跨端联调步骤见 [专项验收](CROSS_END_ACCEPTANCE.md)。

仅在空管理员表时，用 `POST /api` 提交：

```json
{"action":"admin.bootstrap","data":{"bootstrapToken":"一次性口令","username":"owner","password":"至少10位的独立强密码"}}
```

成功后删除或轮换 `ADMIN_BOOTSTRAP_TOKEN`。通过后台创建商品和 SKU、设置运费与禁售地区、上传真实图片、发布课堂内容。新库默认无商品；不会自动导入虚构商品进入生产环境。

## 超时订单任务

每分钟由受信任调度器向 `POST /api` 提交 `{"action":"internal.expireOrders","data":{}}`，并带请求头 `X-Job-Secret: <INTERNAL_JOB_SECRET>`。服务会查单、关单并释放超过 30 分钟仍未支付的预占库存。不要把任务密钥放到小程序或 PC 后台。

## 上线验收

在真实商户环境完成一笔低额支付、发货同步、确认收货，以及另一笔原路退款。验证重复支付、支付回调重放、断网后主动查单、超时关单、禁售地区、库存并发、管理员越权和敏感数据脱敏。生产支付回调、发货管理和退款全部验收通过后，再提交微信审核。详细清单见 `docs/ACCEPTANCE.md`。


## 首页、库存、物流与隐私政策

- 已有数据库也需重新执行 `backend/schema.sql`，新增 `shipment_tracking` 表；其他配置复用 `configs`，不覆盖已有商品或订单。
- 超级管理员在“运营配置”设置首页 1–5 个轮播位、推荐商品/文章顺序、果园介绍及合作入口。推荐内容必须已发布；下架后小程序过滤该项。留空的推荐列表分别使用前三件商品和最新两篇文章。
- 在“商品与库存”点击“批量调整库存”，选择规格、填写调整后的可售库存及原因。接口 `admin.inventory.batch` 使用 `expectedStock` 校验并锁定规格，任何冲突整批回滚；预占库存不变。刷新库存后再提交冲突批次。操作明细和原因与库存变更同一事务写入审计日志。
- 服务端设置 `KUAIDI100_CUSTOMER`、`KUAIDI100_KEY`（快递100企业版凭据），不在前端配置；订单详情与后台支持查看轨迹。支持 SF、YTO、ZTO、STO、YD、EMS、JD、JT；微信承运商编码映射为快递100编码。顺丰查询仅传收货手机号尾四位。当前使用主动查询，半小时复用同运单缓存，不配置物流推送回调。查询结果仅展示，不自动改变订单收货状态。
- 无凭据、未揽收、异常或不支持的承运商均展示明确提示，不伪造轨迹；可复制运单号查询。微信发货信息同步与快递轨迹查询是两项独立服务。
- 隐私正文在 `docs/PRIVACY_POLICY.md`，服务端默认正文嵌入 `backend/privacy.json`。在“运营配置—隐私政策”填写营业执照主体全称、联系地址、隐私事务邮箱/电话并核实实际服务商、安全措施及留存机制后发布。缺少主体信息不能发布；未发布草稿不会通过公开接口展示。小程序“我的”、收货地址、结算和合作表单均可打开政策页面。
- 微信公众平台“用户隐私保护指引”需按实际收集内容单独填写，并完成平台授权审核。网站政策页不能代替平台配置。
- 演示后台配置在当前浏览会话保存；演示小程序使用内置配置及政策预览，双方并不共享数据库。跨端验收请关闭演示模式并指向同一后端。
## 收藏、到货订阅消息与降价提醒

上线前执行 `backend/schema.sql` 中的 `CREATE TABLE IF NOT EXISTS product_attention`，再部署新版后端和小程序。接口 `product.attention.set` 使用登录用户身份保存或取消关注，`product.attention.list` 仅返回当前用户的已发布商品。收藏与提醒分开保存；只有整款无可购买规格时可新增到货提醒，补货后可取消提醒或查看购买。

到货状态在用户进入「我的」或「到货提醒」时根据最新可售库存刷新。销量取 SKU 的 `sold` 字段，已付款增加、完成退款减少；演示数据单独标记。验证收藏跨页面同步、重复点击、取消、售罄设置提醒及补货后显示已到货。

微信到货推送部署步骤：

1. 先执行 `backend/schema.sql` 中 `restock_notifications` 建表语句，再部署后端和小程序。
2. 在微信公众平台选用当前类目允许的一次性到货订阅模板；设置 `WECHAT_RESTOCK_TEMPLATE_ID` 和 `WECHAT_RESTOCK_TEMPLATE_FIELDS`。映射示例为 `{"thing1":"product","thing2":"remark","time3":"restockedAt"}`，实际字段编号须与模板一致。支持商品名称、到货提示和时间，版本沿用 `WECHAT_MINIPROGRAM_STATE`，凭据沿用服务端 `WECHAT_APP_ID` / `WECHAT_APP_SECRET`。
3. 每分钟由受信任调度器向 `POST /api` 提交 `{"action":"internal.restockNotices","data":{}}`，带 `X-Job-Secret: <INTERNAL_JOB_SECRET>`。每次最多处理100条，仅发送已授权、仍在关注、已发布且存在可售 SKU 的商品；商品保存、库存调整、取消订单或退款恢复库存均由同一任务检测。
4. 用户设置到货提醒后，在商品详情或到货提醒列表主动点击「订阅微信到货提醒」。同意授权才登记发送资格；拒绝、接口不可用或未配置模板仍保留站内提醒。演示模式不调用微信授权或发送接口。

`restock_notifications.push_status` 记录 `READY`、`SENDING`、`SENT`、`FAILED`、`BLOCKED`、`UNCERTAIN`、`CANCELLED`；`push_error` 保存失败原因。条件更新保证并发任务仅领取一次；成功后不再发送，明确失败由下次任务重试，43101需用户重新授权。网络结果不确定或服务在发送时中断不会自动重试，需结合微信结果人工核实。取消提醒会撤销未发送的任务；已经领取的发送可能仍会完成。取消后重新设置需要重新订阅；待核实的旧任务须先处理。修改模板后旧模板资格不会自动转移，需取消提醒并重新授权。到货时间为任务检测时间，消息不保证用户打开时仍有库存。

上线验收需使用实际模板和真机：授权后补货收到一次消息、点击跳转商品、重复任务不重复发送、取消后不发送、拒绝授权仍可看站内状态、下架商品不发送。接口参见[微信订阅消息授权](https://developers.weixin.qq.com/miniprogram/dev/api/open-api/subscribe-message/wx.requestSubscribeMessage.html)和[订阅消息发送](https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/mp-message-management/subscribe-message/sendMessage.html)。

收货地址上线配置：在微信小程序后台的用户隐私保护指引中声明收货人姓名、手机号及收货地址，并按账号实际情况完成收货地址接口权限配置。代码已在 app.json 声明 chooseAddress。将后台已有隐私政策同步更新为支持微信选址导入的文案；代码内草案不会覆盖已发布的数据库政策。真机验证首次授权、拒绝、取消和再次导入。

### 降价提醒部署

1. 执行 `backend/schema.sql` 中的 `price_drop_notifications` 建表语句，沿用 `product_attention` 表，无需改变旧表字段。
2. 在微信公众平台选择当前账号类目可用的降价一次性订阅模板，设置 `WECHAT_PRICE_DROP_TEMPLATE_ID` 与 `WECHAT_PRICE_DROP_TEMPLATE_FIELDS`。示例映射为 `{"thing1":"product","thing2":"sku","amount3":"baselinePrice","amount4":"currentPrice","time5":"detectedAt"}`，需按实际模板修改编号；支持 `thing` 字段的商品名、规格名、提示，`amount` 字段的关注价和现价，以及 `time` 字段的检测时间。未配置时保留站内提醒。
3. 由受信任调度器每分钟向 `POST /api` 提交 `{"action":"internal.priceDropNotices","data":{}}`，带同一 `X-Job-Secret`。每次最多100条，仅发送已授权、仍关注、已发布且同一规格可购买、实售价低于关注价的商品。
4. 用户在详情选择规格后设置降价提醒，再主动订阅微信消息；收藏列表提供详情入口，「我的 → 降价提醒」展示关注价、现价、降幅、可购买状态与发送状态。关注价由服务端在设置时读取，单位为分；不比较划线价、其他规格或优惠券后的价格。每用户每商品关注一个规格，重复设置保留原规格及价格，切换规格需取消后重新设置。规格停用时保留提醒并显示规格下架，整款下架时列表暂不展示。

降价推送与到货推送使用独立模板和发送资格。`price_drop_notifications.push_status` 新增 `NONE` 表示仅站内提醒，其余发送状态与到货相同。每个有效提醒成功发送最多一次；明确失败自动重试，授权拒绝需重新订阅，网络结果不确定不自动重试。取消撤销未领取任务，已领取的发送可能完成；`SENDING` / `UNCERTAIN` 旧任务须人工核实后才能更新规格和关注价。模板变更需取消并重新设置、重新授权。站内降价状态按进入页面时价格更新，微信消息为检测时价格，用户打开时可能已变化。

验证命令：`npm run test:restock`、`npm run test:price-drop`、`npm run test:shop`、`npm run mini:typecheck`，以及在 `backend` 下执行 `go test ./...`。上线前真机验证授权、拒绝、同规格降价只收一次消息、取消停止推送、其他规格降价不触发、售罄不推送及点击消息跳转。自动化测试使用模拟微信与 SQL 驱动，未代替真实模板和数据库联调。

## 售后举证与部分退款

已有数据库执行 `backend/schema.sql` 的 `refund_evidence` 建表语句，无需修改原退款表。图片通过已登录的云托管 API 上传并保存在私有数据库表，不需要公共素材域名；单张最大512KB，一次申请最多6张。未提交图片最多保留30张/订单，用户删除图片会移除未提交的服务器副本；再次上传时清理此订单超过一天的未提交图片。运营需按隐私政策定期清理无用资料。

先在验收环境验证未发货全部退款、已发货部分退款、已收货售后、部分退款后剩余金额退款及重复通知。退款接口保留原实付总额作为 `amount.total`，每次申请金额作为 `amount.refund`；重试复用原退款单号。微信部分退款次数及时间间隔要求见[微信支付退款申请文档](https://pay.wechatpay.cn/doc/v3/merchant/4012791862)。

## 再次购买、订单搜索与发货通知

订单列表和详情可再次购买，结算以当前发布的商品规格为准。缺货或下架提示后跳过；库存或限购不足调整数量并要求确认。结算再次刷新商品，不从购物袋覆盖此次选择的数量，不复用历史优惠券、地址和配送日期。

`order.list` 与 `admin.order.list` 接收 `keyword`、`filter`、`limit`、`offset`，搜索订单号、商品名称快照及规格，多关键词必须全部命中。搜索和状态过滤发生在数据库分页之前。用户接口限定登录用户身份；微信前端每页20条，后台每页50条。后台订单搜索不影响独立的退款申请列表。

上线前执行 `backend/schema.sql` 中的 `shipping_notifications` 建表与旧订单补录语句，再部署后端和小程序。补录只生成站内通知，不补发微信消息；重复执行不重置已读状态。新发货与通知在同一事务中保存；微信物流同步失败仍显示站内发货信息，重试物流同步不会新增通知。通知列表优先展示未读，最多200条，未读总数独立统计。

要开通微信推送，先在微信公众平台选择适合当前类目的「订单发货通知」一次性订阅模板，并在云托管配置：

- `WECHAT_SHIPPING_TEMPLATE_ID`：真实模板ID，缺省时仅显示站内通知。
- `WECHAT_SHIPPING_TEMPLATE_FIELDS`：JSON格式的模板字段与数据来源映射，例如 `{"character_string1":"orderNo","thing2":"carrier","character_string3":"trackingNo","time4":"shippedAt"}`。字段编号须与所选模板完全一致，不能直接照抄示例编号。支持来源 `orderNo`、`product`、`carrier`、`trackingNo`、`shippedAt`，分别映射为字符串、事物或时间字段。商品和承运商限制20字符，订单号或运单号超过32字符时明确报错并保留完整站内信息，时间按北京时间生成。
- `WECHAT_MINIPROGRAM_STATE`：`developer`、`trial` 或 `formal`，默认为正式版。
- 沿用 `WECHAT_APP_ID`、`WECHAT_APP_SECRET`；完成微信服务端接口所需IP白名单等账号设置。

用户付款后在订单详情点击「订阅微信发货通知」并同意微信授权，才记录此订单的一次发送资格。拒绝或取消不会影响支付与站内通知；站内已读也不取消已订阅的推送。授权后恰好发生发货仍会尝试发送。同一订单通过条件更新仅领取一次发送任务，成功后不会再次发送。明确错误记录为 `FAILED` 并允许后台重试；用户未授权返回 `BLOCKED`，由用户重新订阅。网络失败等无法判断微信是否收到的请求保留 `UNCERTAIN`，不会自动重试；服务在发送中断开可能保留 `SENDING`，需人工核实。微信物流同步与订阅消息发送是独立状态。

演示模式下，订单详情提供「演示订阅发货通知」与「模拟发货」，消息只保存在本地，便于验证通知列表与已读状态。演示后台与小程序仍使用独立数据。

接口依据：[微信订阅消息发送接口](https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/mp-message-management/subscribe-message/sendMessage.html)及[微信订阅授权接口](https://developers.weixin.qq.com/miniprogram/dev/api/open-api/subscribe-message/wx.requestSubscribeMessage.html)。线上验收需使用当前账号实际模板和真机验证；本地模拟不能证明真实推送已开通。


经营概览访问与加购统计：先执行 `backend/migrations/20261008_commerce_analytics.sql`，再发布后端、小程序与后台。旧版小程序不采集，历史数据不补算；重复迁移保留采集开始时间。详见 [经营统计说明](DASHBOARD_ANALYTICS.md)。
