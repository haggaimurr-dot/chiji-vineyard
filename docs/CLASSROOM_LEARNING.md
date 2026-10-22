# 种植课堂：系列课程、学习进度与课堂问答

课堂新增「系列课程」「学习进度」「课堂问答」入口，保留全部内容、收藏及阅读记录。

系列课程使用独立课程实体，在后台「种植内容 → 课程编排」管理标题、简介、阶段、品种、发布状态、展示顺序和课时顺序。支持添加、移除和上下移动课时，同一文章可加入多门课程，总体进度按文章去重。课时最多200节，发布课程只允许关联已发布内容；课程保存校验版本，防止覆盖其他工作人员的修改。仅已发布内容进入目录与进度分母，草稿、待发布、下架内容不计入。

内容页新增「标记学完」，支持撤销。打开内容只产生原有阅读记录，完成状态由学员主动确认。学习进度展示总体已学完节数、百分比和各系列进度；「继续学习」定位本系列第一节未完成内容。进度在服务端按微信身份保存，演示模式在独立本地存储保存。视频无需看完才能手动标记。播放器每10秒保存当前时间点，暂停、播完和离开时补报，下次打开从已保存的位置续播；已播放至末尾时重新从头播放。视频时间点与手动完成状态分别保存，不自动标记学完。进度按微信身份隔离，并绑定原始视频地址，替换视频后从头开始。网络失败时保留待报数据，后续播放事件或暂停时重试；退出前仍未成功上报的最后几秒可能丢失。

学员在内容页提交问题（最多500字），可在该内容页或课堂问答入口查看自己的提问、待回复状态和老师答复。问题与回复仅提问者及有权限的后台人员可见。问答每页20条，支持继续加载、加载失败重试；问题提交失败保留输入，同一次提交重试沿用事件编号防止重复。后台「种植内容 → 课堂问答」支持待回复、已回复和全部筛选，回复最多2000字；查看沿用 `article.read` 权限，回复沿用 `article.write` 权限。回复更新校验旧答复，防止覆盖其他工作人员的新回复。

上线前执行 [20261007_classroom.sql](../backend/migrations/20261007_classroom.sql)，建立 `classroom_progress`、`classroom_questions`；新数据库完整 schema 已包含这两张表。随后同步发布后端、小程序和后台。小程序与后台的演示数据各自独立，不互通；正式模式通过同一后端接通提问与回复。

新增接口：`classroom.courses`、`classroom.complete`（articleId、completed）、`classroom.questions`（articleId 可选、status 可选、offset、limit）、`classroom.ask`（articleId、question、eventId）；后台 `admin.classroom.questions`、`admin.classroom.answer`（id、answer、expectedAnswer）。`article.detail` 增加当前用户 completed。

验证命令：`npm run test:classroom`、`npm run test:content`、`npm run check`；后端执行 `go test ./...`。数据库迁移、真实微信账号隔离及开发者工具中的 WXML 交互需在部署环境验收。

2026-10-07 本地验证通过：课堂原有功能及新增系列功能测试、内容回归测试、小程序类型检查与编译、后台类型检查与生产构建、后端完整测试。后台浏览器验证覆盖提交回复、待回复/已回复切换、刷新后持久保存，以及1360和1024宽度布局，无浏览器脚本错误。启动后台开发服务器后可执行 `npm run test:classroom-ui`（默认地址 `http://127.0.0.1:5197`，可用 `ADMIN_UI_URL` 指定）。构建仍提示现有主包超过500KB。以上为本地验证，未执行正式数据库迁移或真实微信端验收。

## 原有搜索、收藏与学习记录

种植课堂提供「全部内容」「我的收藏」「学习记录」。搜索匹配标题、摘要、阶段及品种，可与阶段、品种和当前列表组合使用；输入300毫秒后搜索，键盘确认或按钮立即搜索，支持清空。每页20条，触底或点击加载更多；追加失败保留已有内容并可重试。

列表和详情页均可收藏、取消收藏，返回课堂后刷新状态。个人中心增加「课堂收藏」「学习记录」快捷入口。收藏按收藏时间倒序，重复收藏不改变时间。

内容成功加载并显示后自动记录学习历史，同一文章合并为一条，按最近阅读排序，时间采用北京时间。沿用幂等阅读事件，不把列表浏览、未显示或加载失败记为学习。记录表示打开阅读，不代表读完文章或看完视频；视频播放时间点单独保存。记录上报失败不影响阅读，返回详情时重试。下架、草稿及未到发布时间的内容不出现在个人列表中。

正式模式以服务端微信身份隔离收藏，以已有阅读事件中的读者哈希隔离学习记录。演示模式使用独立本地存储，重启后保留，不进入正式数据库。

上线前执行 `backend/schema.sql` 中的 `article_favorites` 建表语句。新数据库已包含学习历史查询索引；已有数据库需检查索引后执行一次：

```sql
ALTER TABLE article_reads ADD INDEX idx_article_reads_reader (reader_hash,article_id,created_at);
```

同步发布后端和小程序。新增接口：`article.search`（keyword、stage、variety、view、offset、limit），返回 articles 和 hasMore；`article.favorite.set`（articleId、enabled）。`article.detail` 增加当前用户 favorite。旧 `article.list` 保留供首页等现有调用使用。

本地验证：`node tests/classroom-learning.cjs`、`npm run test:content`、`npm run mini:build`；后端在 `backend/` 执行 `go test ./...`。真实微信账号隔离、云数据库迁移及开发者工具交互需在部署环境验收。

## 2026-10-08 课程实体、视频进度与独立审核权限

已有数据库依次执行 `20261007_classroom.sql`（未执行时）和 [20261008_content_classroom.sql](../backend/migrations/20261008_content_classroom.sql)，再同步发布三端。新数据库使用完整 schema，后台新建课程。迁移新增 `classroom_courses` 和 `classroom_video_progress`；已有已发布内容按旧阶段和品种转换一次为独立课程，保留旧目录顺序。重复迁移不会覆盖已编辑课程。迁移后新文章不会自动加入课程，需在课程编排中添加。

新增 `admin.course.list`、`admin.course.save`（course、expectedVersion），权限分别为 `article.read`、`article.write`；`classroom.courses` 仅显示已发布课程及其已发布课时。新增 `classroom.video.get`（articleId）及 `classroom.video.save`（articleId、source、position、duration），时间以秒计。source 使用文章原始 videoUrl，云视频不使用临时地址；服务端锁定文章确认状态和地址后保存。

`admin.article.review` 改为独立 `article.review` 权限。内容编辑、提交审核和发布仍使用 `article.write`；审核员配合 `article.read` 可预览及审批，不获得编辑权。显式仅有编辑权限的账号不能审批，超级管理员可在账号权限设置中单独授权审核。无权限记录的历史运营账号仍沿用全部业务权限，需要限制的账号应保存显式列表。

演示课程及视频进度各自在本端本地存储，不代表正式三端同步。
