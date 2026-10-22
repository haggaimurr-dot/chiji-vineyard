const {chromium}=require('playwright'),assert=require('node:assert/strict'),path=require('node:path'),fs=require('node:fs')
;(async()=>{
 const browser=await chromium.launch({headless:true,executablePath:process.env.PLAYWRIGHT_BROWSER||'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'})
 let page
 try{
  page=await browser.newPage({viewport:{width:1360,height:900}});const errors=[]
  page.on('pageerror',e=>errors.push(e.message))
  await page.goto(process.env.ADMIN_UI_URL||'http://127.0.0.1:5184',{waitUntil:'networkidle'})
  await page.getByLabel('用户名',{exact:true}).fill('qa-products');await page.getByLabel('密码',{exact:true}).fill('Products2026!');await page.getByRole('button',{name:'进入后台',exact:true}).click();await page.getByRole('button',{name:/商品与库存/}).click()
  await page.getByRole('button',{name:'批量导入',exact:true}).click()
  const header='商品分组,商品名称,副标题,分类,销售模式,预计发货日期,封面,产地,规格名称,售价元,库存,限购,重量说明'
  const csv=header+'\r\n'+Array.from({length:125},(_,i)=>`G${i},验收商品${String(i).padStart(3,'0')},分页验收,family,spot,,/demo-products/shine.jpg,测试果园,家庭装,89.90,20,5,2kg`).join('\r\n')+'\r\n'
  await page.locator('input[type=file]').filter({visible:true}).setInputFiles({name:'invalid.csv',mimeType:'text/csv',buffer:Buffer.from(csv.replace('89.90','89.999'))})
  await page.getByText('第 2 行：名称、封面、分类、模式、售价、库存或限购无效',{exact:true}).waitFor();assert.equal(await page.getByRole('button',{name:/确认创建/}).isDisabled(),true)
  await page.locator('input[type=file]').filter({visible:true}).setInputFiles({name:'products.csv',mimeType:'text/csv',buffer:Buffer.from('\ufeff'+csv)})
  await page.getByRole('button',{name:'确认创建 125 款草稿',exact:true}).click();await page.getByText('已创建 125 款草稿、125 个规格',{exact:true}).waitFor()
  await page.getByRole('button',{name:'商品列表',exact:true}).click();await page.getByLabel('搜索商品',{exact:true}).fill('验收商品');await page.getByText('共 125 款商品',{exact:true}).waitFor()
  await page.getByRole('button',{name:'下一页',exact:true}).filter({visible:true}).click();await page.getByText('第 2 页',{exact:true}).filter({visible:true}).waitFor()
  await page.getByLabel('搜索商品',{exact:true}).fill('验收商品124');await page.getByText('共 1 款商品',{exact:true}).waitFor();await page.getByText('验收商品124',{exact:true}).waitFor()
  // Save an existing product and verify the edit drawer clones reactive data.
  await page.getByRole('button',{name:'编辑',exact:true}).click();await page.getByLabel('商品名称',{exact:true}).fill('验收商品124更新');await page.getByRole('button',{name:'保存商品',exact:true}).click();await page.getByText('验收商品124更新',{exact:true}).waitFor()
  await page.getByRole('button',{name:'批次',exact:true}).click();await page.getByRole('button',{name:'新增批次',exact:true}).click()
  await page.getByLabel('批次号',{exact:true}).fill('QA-BATCH-01');await page.getByLabel('果园 / 地块',{exact:true}).fill('验收果园');await page.getByLabel('采收日期',{exact:true}).fill('2026-10-01');await page.getByLabel('包装日期',{exact:true}).fill('2026-10-02');await page.getByLabel('有效期至',{exact:true}).fill('2026-10-08');await page.getByRole('button',{name:'保存批次',exact:true}).click();await page.getByText('QA-BATCH-01',{exact:true}).waitFor()
  await page.getByRole('button',{name:'编辑 / 归档',exact:true}).click();await page.locator('.drawer').getByLabel('批次状态',{exact:true}).selectOption('ARCHIVED');await page.getByRole('button',{name:'保存批次',exact:true}).click();await page.locator('.tools-panel tbody').getByText('已归档',{exact:true}).waitFor()
  await page.reload({waitUntil:'networkidle'});await page.getByRole('button',{name:/商品与库存/}).click();await page.getByRole('button',{name:'批次管理',exact:true}).click();await page.getByText('QA-BATCH-01',{exact:true}).waitFor();await page.locator('.tools-panel tbody').getByText('已归档',{exact:true}).waitFor()
  await page.setViewportSize({width:1024,height:900});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  const artifacts=path.join(__dirname,'artifacts');fs.mkdirSync(artifacts,{recursive:true});await page.screenshot({path:path.join(artifacts,'product-batches.png'),fullPage:true})
  await page.getByRole('button',{name:'商品列表',exact:true}).click();await page.getByLabel('搜索商品',{exact:true}).fill('验收商品');await page.getByText('共 125 款商品',{exact:true}).waitFor();assert.equal(await page.locator('.inventory-panel').isVisible(),false);assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);await page.screenshot({path:path.join(artifacts,'product-pagination.png'),fullPage:true})
  assert.deepEqual(errors,[]);console.log('Product management UI passed: CSV row errors and disabled commit; 125 drafts imported; search/pagination past 100; product edit; batch create/archive; persistence; 1024px layout; no browser errors.')
 }catch(e){if(page){console.error((await page.locator('body').innerText()).slice(-4000));await page.screenshot({path:path.join(__dirname,'artifacts','product-management-failure.png'),fullPage:true})}throw e}finally{await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1})
