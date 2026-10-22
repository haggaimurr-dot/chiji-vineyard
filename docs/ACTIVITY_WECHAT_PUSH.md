# 活动微信开始提醒

正式模式下，预约成功的用户可在活动专题页点击「订阅活动微信提醒」。用户同意后登记一次提醒；拒绝、接口失败或登记失败均不改变预约。候补用户需转正后主动订阅。演示模式不申请微信授权、不发送消息。

在活动开始日期的北京时间当天发送，消息展示活动标题、地点、开始日期及提示，点击进入对应活动专题。开始日期已过不再接受新订阅。当前不推送候补转正、取消及内容变更消息，这些仍沿用站内通知。

每个用户、活动最多成功发送一次。发送前在首页配置锁内重新核对活动存在、预约开放、开始日期及当前预约记录。取消或取消后重新预约不会继承旧预约的授权登记，需重新确认订阅；已发送或结果不确定的记录不自动重置。任务并发运行时以 READY → SENDING 的条件更新认领发送，避免重复推送。认领后发生取消或活动变更，已发出的请求无法撤回。

发送状态：NONE 未登记，READY 待发送，SENDING 已认领，SENT 已发送，FAILED 微信明确失败，BLOCKED 微信拒收，UNCERTAIN 网络或响应结果不明。FAILED/BLOCKED 必须由用户再次授权确认后回到 READY，不自动重试；SENDING/UNCERTAIN 需运维核实，不自动重发。进程在认领后退出可能留下 SENDING。模板更换后原登记不发送，需用户刷新后重新确认。

## 部署

1. 已有数据库执行 `backend/migrations/20261008_activity_push.sql`；新数据库执行完整 `backend/schema.sql`。预约基础表需已部署。
2. 微信后台选择符合活动开始提醒用途的一次性订阅模板，设置服务端 `WECHAT_ACTIVITY_TEMPLATE_ID` 与 `WECHAT_ACTIVITY_TEMPLATE_FIELDS`。字段编号须与实际模板一致；支持 thing 标题/title、地点/location、提示/remark，以及 date/time 开始日期/startDate。示例见 `.env.example`。配置无效时专题页隐藏订阅入口。
3. 配置 `WECHAT_APP_ID`、`WECHAT_APP_SECRET` 和 `WECHAT_MINIPROGRAM_STATE`，发布后端及小程序。所有凭证保留在服务端。
4. 由云定时任务每五分钟调用 `POST /api`，携带 `X-Job-Secret: <INTERNAL_JOB_SECRET>`，正文如下。每次最多处理100名用户，按实际规模调整调度频率；任务必须在开始当天运行，次日不补发。

```json
{"action":"internal.activityNotices","data":{}}
```

返回 scanned/sent/failed/skipped 计数。运维可在数据库查询 `activity_push_subscriptions` 的状态、错误和更新时间；无公开群发接口。微信配置读取、订阅登记均要求已验证的小程序用户身份。删除使用数据或注销账号同时删除订阅记录。

## 验证

```text
npm run mini:build
node tests/activity-push.cjs
npm run test:home-features
cd backend
go test ./...
```

自动化覆盖授权同意/拒绝、登记失败、重复点击、旧响应保护、字段校验、北京时间日期边界、重复任务、预约取消或重建、活动删除及发送拒收/不确定结果。上线前还需真实 MySQL 和微信真机验收：选择实际模板、授权、定时发送、点击消息进入专题、拒收及取消预约。当前自动化使用数据库与微信接口模拟，不代表真实微信送达已验证。

接口参考：[客户端订阅授权](https://developers.weixin.qq.com/miniprogram/dev/api/open-api/subscribe-message/wx.requestSubscribeMessage.html)、[服务端订阅发送](https://developers.weixin.qq.com/miniprogram/dev/OpenApiDoc/mp-message-management/subscribe-message/sendMessage.html)。
