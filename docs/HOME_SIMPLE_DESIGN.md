# 首页简化：果园里的日常

日期：2026-10-10。首页改为安静的果园介绍，重点是正在采摘的人和一小段朴实文字。

## 展示顺序

1. 简短品牌文字与单张果园照片。图片不自动轮播、不叠加促销文案。
2. 果园介绍标题与正文，使用米白背景、灰绿文字和较充足留白。
3. 当季葡萄、种植手记、联系果园三个列表入口，分别跳商城、种植课堂、销售合作。
4. “看看果园近况”默认收起。点击后显示原有轮播、公告、活动及预约、推荐、日历、商品和文章等内容；再次点击收起。

底部五个Tab继续提供直接入口。首页原功能保留，默认首屏不铺陈业务模块。展开状态只在当前页面实例中保留，不写入用户存储。

## 后台如何更换成实拍

- 运营配置→首页运营位：第一张轮播图片用作首页主图，建议上传果园采摘实拍。建议照片中心保留人物、手和葡萄，主图使用接近正方形裁切。
- “果园介绍标题/正文”对应主图下方介绍。后台预览同步采用简化布局；点击“看看果园近况”核对其他内容。
- 其他轮播内容在展开区域继续提供原按钮与目标。图像加载失败显示轻量占位，重新进入/下拉刷新可重试。
- 正式模式未配置图片时显示准备中的占位，不以演示图代替果园实拍。后台已有保存配置仍优先于默认数据，需要运营人员自行更换；本次没有写入真实数据库。

## 图片来源与文件

内置imagegen生成的采摘场景示意，不能当作本果园农民实拍。小程序主图下标注“采摘场景示意”。演示模式使用；正式模式若显式引用同一打包示意路径也保留标识。

- 小程序资源：`miniprogram/assets/home/harvest-scene.jpg`。
- 后台演示资源：`admin/public/assets/home/harvest-scene.jpg`。
- JPEG 1122×1402，268669字节（约262KB）；源PNG留存在Codex生成目录。两端使用相同JPEG副本。
- 模式：内置imagegen；生成后转JPEG用于小程序，不改写原始PNG。

最终生成提示词：

```text
Use case: photorealistic-natural. Asset type: hero photograph for a Chinese family vineyard WeChat mini program. Create a quiet, candid documentary-style photograph of a middle-aged Chinese farmer working in a grape vineyard, wearing ordinary faded blue cotton work clothes and a simple straw hat, carefully harvesting one ripe bunch of green grapes using small pruning scissors. Natural ordinary appearance, slightly weathered hands, attention on the work, no posing or looking at the camera. Rows of real grape vines and leafy trellis, a simple harvest basket in the lower part. Soft morning daylight filtered through leaves, honest imperfect rural textures, muted olive greens and warm earth tones, subtle natural film grain. Portrait 4:5 composition, farmer and hands clearly legible in the central area so a mobile cover crop preserves the face and harvesting action. A little breathing room, restrained and warm editorial photography, trustworthy everyday farming rather than luxury advertising. No written text, no logo, no watermark, no artificial glow, no glossy promotional styling.
```

## 验证

- `npm run mini:build`：小程序TypeScript编译并生成JavaScript。
- `npm run admin:build`：后台类型检查与生产构建通过，仍有既有大包警告。
- `node tests/home-features.cjs`：首页逻辑、默认收起、展开/收起、当前与旧图片失败、刷新重试、预约与候补、推荐回归。
- `node tests/cross-end-updates.cjs`：配置刷新、图片替换、旧响应、防止下架跳转等模拟传输回归通过。
- `node tests/home-ui.cjs`：后台默认折叠、展开后素材、图片上传/保存/回读/刷新、失败图片、1360/1024视口通过。
- `node tests/operations-config-ui.cjs`：运费模板与活动保存、排序、删除、保存预览展开、刷新持久化通过。

演示图片保存白名单原先只允许三款商品图，导致新照片保存被拒绝；已在`admin/src/mock-config-validation.ts`仅增加此张打包示意图的精确路径，不放宽正式HTTPS图片规则。测试先发现此失败，修正后专项通过。

### 手机尺寸版式预览

浏览器预览使用当前WXML/WXSS转换和真实Page处理函数，检查390×844与320×844；三个入口对应实际Tab路径，展开/收起不删除原模块。查看截图确认人物面部、手和葡萄在裁切后可见，照片和介绍之间无重叠，页面无横向溢出。页面按纵向滚动设计，完整入口区域在介绍下方。

这是浏览器近似渲染，模拟顶部栏与Tab栏，不是原生微信截图。字体、系统栏和原生组件仍需微信模拟器/真机确认。

![390宽度首屏](test-evidence/2026-10-10-home/home-390.png)

![320宽度首屏](test-evidence/2026-10-10-home/home-320.png)

证据：[浏览器预览结果](test-evidence/2026-10-10-home/browser-preview-results.json)。

### 微信开发者工具限制

CLI已打开项目并开启9421端口自动化，但`currentPage`15秒超时；第二次连接也超时。没有取得原生页面数据或截图，不记为微信运行验收通过。已停止本次无响应的自动化脚本，项目代码未为绕过工具问题修改私有配置。

证据：[原生工具尝试](test-evidence/2026-10-10-home/native-results.json)。
