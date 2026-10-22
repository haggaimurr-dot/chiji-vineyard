# 迟记葡萄园 · 独立微信小游戏第一版

现已增加 [第三人称下地与闯关原型](../docs/VINEYARD_GAME_FIELD_IMPLEMENTATION.md)：四人物预览、七步定植，以及成熟园的雨前采收与交货。构建后可在 [下地体验](http://127.0.0.1:5199/field.html) 试玩。微信默认演示入口为新体验；设置 src/config.js 的 experience 为 classic 可进入下文的旧版经营界面。两者存档独立，新的三维关卡暂未接入正式后端。

这是单独的微信小游戏工程。葡萄园商城小程序通过微信官方接口跳转进入，游戏使用自己的 AppID、登录与存档。

首版采用微信原生 Canvas 2D，包含四块已成园地块、84 日生长季、六类农事、采收分级、库存保鲜、两种销售渠道、三种设施、手记、季末复盘与下一季。游戏采用教学参数，尚未针对真实园区的品种和地区校准。

## 先试玩

在仓库根目录执行：

~~~powershell
npm install
npm run game:build
npm run game:preview
~~~

打开 [本机试玩](http://127.0.0.1:5199)。默认是演示模式，进度保存在当前设备；重开需要玩家确认。浏览器预览与微信版本使用同一套模型、控制器和 Canvas 界面，预览仅用于开发与试玩。

## 在微信开发者工具运行

1. 新建或导入“小游戏”项目，项目目录选择本文件所在的 minigame 文件夹。
2. 在 project.config.json 填写已注册的小游戏 AppID。当前 touristappid 是开发占位配置；无法使用体验身份时，需要真实小游戏 AppID。
3. 保持 src/config.js 的 demo:true 即可运行本机演示，不需要支付、云环境或登录凭据。
4. 注册和发布小游戏后，在商城项目的 miniprogram/data/vineyard-game.ts 填写同一个小游戏 AppID；开发、体验或正式版本分别选择 develop、trial、release。
5. 使用微信客户端验证实际跳转与前后台恢复。浏览器测试和原生 API 模拟测试不代替微信真机验收。

浏览器预览、构建脚本和测试样本已在小游戏 packOptions 中排除，提交包不携带这些开发文件。

## 怎么玩

- 开园后先选择地块，整枝修剪、观察水分；施肥需考虑时机和已有营养。
- 每个回合可以管理多块地，点击“推进 7 天”进入下一回合；确认前会提示天气、生长与仓库损耗。
- 坐果后适度疏果，控制留果量。冠层过密、留果过多、缺水或过量施肥都会影响生长与成熟。
- 进入成熟阶段后，糖度达到游戏门槛才能采收。采收结果按粒重、糖度与损伤分级，计入鲜果库存。
- 到仓库选择批发或零售，销售收入扣除渠道成本后进入游戏账本。库存放置会损耗，冷库可减缓损耗。
- 到第 84 日查看一季复盘。售空库存后可开始下一季，保留经营币、设施和树势。
- 离线每现实小时补算一模拟日，余下不足一小时保留。手动推进回合会重新设定离线计时起点。

经营币、游戏库存和虚拟销售只属于游戏。实际商城库存、付款订单及发货继续由商城处理。

## 切换正式服务

先配置并部署现有 Go 服务，再切换游戏配置：

~~~javascript
// src/config.js
module.exports = {
  demo: false,
  apiBaseURL: 'https://你的后端域名',
  storageNamespace: 'chiji_vineyard',
  requestTimeoutMs: 15000
};
~~~

后端环境增加：

- WECHAT_GAME_APP_ID：独立小游戏 AppID，不能等于商城小程序 AppID。
- WECHAT_GAME_APP_SECRET：小游戏 AppSecret，仅放在服务端。
- TOKEN_SECRET：沿用现有服务的会话签名设置。
- MYSQL_DSN：沿用现有 MySQL 配置。

先执行 auth_sessions 的既有迁移（如果环境尚未升级），再执行 backend/migrations/20261012_vineyard_game.sql。新数据库可执行最新 backend/schema.sql。小游戏请求使用 HTTPS POST /game-api，在微信后台配置所使用的合法 request 域名。

正式模式使用小游戏 wx.login 换取独立 game 会话，由 Go 计算生长、收成、库存、销售及资源变化。数据库事务完成会话校验、农场行锁、版本校验和请求号防重。客户端只发送操作，不上传可信产量或余额。网络响应不确定时保存原请求号，下次重试先核实，避免重复扣币和采收。

正式模式不会在服务不可用时切到本机演示。演示和正式数据使用不同存储键。第一版游戏账号独立，跳转只传入口信息，不携带商城登录令牌；没有实现两端账号绑定或资产合并。

## 接口

统一请求：{ action, data }，正式鉴权为 Authorization: Bearer 的 game 会话。响应沿用 { ok, data } 或 { ok:false, error:{code,message} }。

| action | data | 结果 |
| --- | --- | --- |
| game.login | {code} | 独立小游戏 token |
| game.bootstrap / game.sync | {} | 当前服务端 state |
| game.action | {requestId,expectedVersion,command} | 动作后的 state |
| game.history / game.report | {} | 手记或季末报告及 state |
| game.logout | {} | 撤销当前 game 会话 |
| game.reset | {requestId,expectedVersion,confirm:"清空游戏进度"} | 新农场与递增版本 |

command 类型包括 water、fertilize、prune、thin、protect、harvest、sell、upgrade、advance、newSeason。农事使用 plotId，销售使用 channel，设施使用 facility。服务端严格校验字段和操作窗口。

## 构建与验证

~~~powershell
npm run game:build
npm run test:game
npm run test:game-ui
~~~

UI 测试连接已运行的预览服务；可用 VINEYARD_PREVIEW_URL 指定地址。测试证据位于 docs/test-evidence/2026-10-12-vineyard-game。

后端在 backend 目录执行：

~~~powershell
go test ./...
go vet ./...
~~~

Go 与 JavaScript 用同一组完整状态样本核对农艺及经营结果。真实数据库并发测试 TestVineyardGameMySQLConcurrency 需要 MYSQL_TEST_DSN，创建专用临时测试库并在结束后删除，未设置时跳过。

首版参数统一保存在 src/rules.js 和 Go 对应规则中。调整规则时同步修改两端、重生成共享样本并运行全部相关测试，不能只改客户端数值。

## 本版范围

已实现独立可玩工程和正式服务接口。公开发布前还需要真实小游戏 AppID、服务端凭据与 HTTPS 环境、微信真机验证及当前账号所需的发布资质。

后续可按实际园区记录校准品种、物候、水分与品质，增加连续天气剧本、农事教程关联、更丰富的地图和设施，再评估是否迁移到 Cocos 工作流。
