# Go / MySQL 数据模型

## 配置版本与操作日志

`configs` 继续保存当前生效配置。新增 `config_versions` 保存四种配置 `shipping/coupons/home/privacy` 的完整快照、配置内递增版本号、动作 `BASELINE/SAVE/ROLLBACK`、回滚来源版本、管理员 ID/用户名和创建时间。旧库首次保存时记录原有生效内容为基线 v1，再记录新内容为 v2；没有历史的配置显示当前版本 0。历史快照不可通过管理接口修改或删除，回滚恢复内容后追加新版本。

所有配置保存和回滚在同一事务内锁定 `configs` 对应行，更新配置、插入版本并记录 `audit_logs`；任一步失败整批回滚。回滚携带 `expectedVersion`，与当前最新版本不同则拒绝。历史数据重新经过当前配置规则校验，首页已下架商品或未发布文章等不能通过历史回滚重新配置。

新增超级管理员接口：`admin.config.versions` 按配置类型分页返回版本与 `currentVersions`；`admin.config.rollback` 接收 `key/versionId/expectedVersion/reason`；`admin.audit.list` 按 `action/username/targetId/startDate/endDate` 筛选日志并分页。日期按北京时间整日查询，结束日期包含当天。日志详情递归隐藏密码、令牌及密钥字段，不开放日志修改/删除接口。

## 业务数据与状态

收藏与提醒：`product_attention.kind` 支持 `favorite/restock/pricedrop`，以微信用户、商品、类型为唯一键。到货推送存于 `restock_notifications`；降价提醒存于 `price_drop_notifications`，以用户和商品为唯一键，记录 `sku_id/baseline_price/template_id/push_status/push_error/updated_at`。降价关注价由服务端锁定已发布商品的启用 SKU 并读取实际价格，以人民币分保存；同规格低于关注价才标记降价，重复设置不重置基线。取消与推送状态更新在同一事务中完成。消息推送需要显式微信授权，并用条件更新领取发送资格，避免重复发送；网络结果不确定时保留待核实状态。

课堂内容的定时任务存于 `article_schedules`，版本快照存于 `article_versions`，幂等阅读事件存于 `article_reads`。文章 JSON 增加 `version`、`scheduledAt`、`updatedAt`；阅读次数和读者人数从事件表汇总，忽略客户端传入值。保存、恢复和到期发布均事务锁定文章并保存新快照，恢复结果为草稿。详细规则见 [内容功能说明](CONTENT_FEATURES.md)。

`addresses` 沿用每用户一行，`payload` 保存地址数组，每条包含 `id`、`isDefault` 和收货信息。旧单地址对象读取时转换为默认地址，首次修改时写入数组，无需修改表结构。地址写入按用户加事务锁，非空列表保留唯一默认地址；删除默认地址时将剩余第一条设为默认。`address.list/save/default/delete` 管理列表，`address.get` 兼容返回默认地址，订单地址保留独立快照。

表结构以 `backend/schema.sql` 为准。`products` 保存商品文案，`skus` 保存以人民币分计价的价格、可售库存、预占库存及销量；`carts`、`addresses` 属于微信用户；`orders` 保存下单时的商品、地址和报价快照；`refunds` 记录每次全部或部分金额退款，`refund_evidence` 私有保存订单举证图片及申请关联；`articles`、`partner_leads` 对应课堂和合作业务；`admins` 与 `audit_logs` 记录后台权限及操作；`configs` 保存运费等配置；`media` 保存后台上传图片。

订单分别维护 `orderStatus`、`paymentStatus`、`fulfillmentStatus`、`refundStatus`。创建订单使用 MySQL 事务和行锁校验 SKU 价格与库存，扣减可售库存并增加预占。支付通知幂等地减少预占、增加销量；取消或到期关单恢复可售库存。已发货、已收货订单可上传举证申请售后。订单 payload 的 `refundedAmount` 记录累计成功退款金额，`PARTIAL` 表示仍有可退余额；同一订单最多一条进行中申请。部分退款保留付款和履约状态；累计退满时，仅未发货订单恢复库存并关闭，已发货订单不回补库存。累计退满时扣回整单销量，部分金额退款无法对应具体件数，暂不扣销量。

所有价格、库存、权限和状态转换由 Go 服务校验。生产数据库禁止浏览器及小程序直连。

合作线索：`admin.lead.list`（`admin.lead.search` 为别名）接收 `keyword/status/channel/region/ownerId/reminder/limit/offset`，返回 `{items,total,channels,regions,limit,offset}`。先搜索和筛选再计数、分页，按数据库提交时间及 ID 降序排列；渠道、地区选项来自全部线索。关键词可用空格分隔，每个词均须命中客户资料、采购需求、负责人、最近备注或历史跟进备注。默认每页20条，最多100条。`ownerId` 空值表示全部，`__unassigned__` 表示未分配；`reminder` 可选 `due/upcoming/done/none` 或空值（全部）。到期和待到期筛选仅包含未成交、未关闭且未完成的提醒。

`partner_leads.payload` 新增 `ownerId/ownerName/reminderAt/reminderDone/version`；旧记录默认为未分配、无提醒、版本0。`admin.lead.owners` 向所有后台角色返回启用管理员的 `id/username/role/active`，不提供密码资料；完整 `admin.account.list` 仍只允许超级管理员。`admin.lead.get {leadId}` 返回最新线索。`admin.lead.reminders` 返回全部未成交、未关闭且有未完成提醒的线索，按提醒时间升序排列，不受列表分页或筛选限制。

`admin.lead.update` 必须带 `leadId` 和非负整数 `expectedVersion`，其余 `status/followUpNote/ownerId/reminderAt/reminderDone` 按需提交，未提交的字段保留。服务端在事务中锁定线索行并比较版本；负责人只能选启用账号，名称由服务端解析。非空跟进备注去首尾空格后最多2000字，每次提交都生成新历史，即使内容相同；空备注不清空最近备注。实际变更使版本加1并同时写入历史和审计日志；没有变更时原样返回，不增加版本或历史。

新增 `partner_lead_history` 表：`id VARCHAR(80)` 为主键，`lead_id VARCHAR(80)` 关联线索，`created_at DATETIME(3)` 与 `payload JSON` 保存不可变操作快照，按线索和时间建立索引。`admin.lead.history {leadId}` 按时间倒序返回 `{id,leadId,actorId,actorName,createdAt,note,status,ownerId,ownerName,reminderAt,reminderDone,changes}`，`changes` 按 `owner/status/note/reminder` 标识该次变更。历史同时兼容旧 `followUps`，以相同ID去重；只有最近备注的旧数据补为时间未知的历史记录。JSON中的 `followUps` 继续作为搜索和旧客户端读取的兼容镜像，`followUpNote` 继续保存最近一条非空备注。

提醒时间接收带时区的 RFC3339 时间并统一保存为 UTC，空字符串清除提醒并恢复未完成标记。新建或重新启用待办提醒必须晚于当前时间；已经到期的既有提醒仍允许标记完成或更新其他字段。成交或关闭线索自动完成保留的提醒，不能新增待办提醒。提醒仅用于后台界面，不产生站外消息或投递任务。

优惠券：`configs` 中 `coupons` 保存 `{items:[{code,name,minimum,amount,startDate,endDate,active}]}`，所有金额为分。`order.quote` / `order.create` 接收 `couponCode`，服务端重新核算 `quote.discount` / `quote.couponCode`。创建订单时锁定优惠券配置行，再检查该用户的已有订单，防止并发重复使用；未支付取消/关闭订单释放，退款不释放。优惠前商品金额决定包邮，运费不抵扣，商品应付最少1分。正式配置默认空，`admin.coupon.save` 仅超级管理员可操作。

`orders.payload.deliveryDate` 保存可选期望日期（YYYY-MM-DD），后端按北京时间限制为两天后至30天内，并不早于所有预售商品的预计发货日期。不填写表示尽快配送。

`invoice_requests` 以 `order_id` 为主键，JSON 保存 `kind/title/taxId/email/status/amount/createdAt` 及可选 `number/reason`。`invoice.apply` 校验用户归属、已支付和退款状态，按订单实付金额创建；`APPLIED → ISSUED/REJECTED` 由 `admin.invoice.update` 执行，退回后可重新申请。申请及后台处理锁订单行避免与退款并发，登记开票时再次校验退款状态。订单读取从独立表附加申请。商家人工开票后上传 PDF，系统自动通过 SMTP 发送附件。`invoice_files` 私有保存 PDF；`invoice.file` 仅申请用户可读，`admin.invoice.file` 仅登录运营人员可读。申请附加 `fileName/issuedAt/emailStatus/emailAttempt/emailStartedAt/emailError/sentAt`；邮件状态为 `READY/SENDING/SENT/FAILED/UNCERTAIN`，成功不重复发送，失败可由后台重试，不确定结果须核实后再重试。
