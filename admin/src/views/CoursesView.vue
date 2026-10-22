<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
import type { Article } from '../types'
import type { CourseEntity } from '../course-demo'
const courses=ref<CourseEntity[]>([]), articles=ref<Article[]>([]), editing=ref<CourseEntity|null>(null), busy=ref(false), error=ref(''), selected=ref('')
async function load(){busy.value=true;error.value='';try{const result=await Promise.all([api<CourseEntity[]>('admin.course.list'),api<Article[]>('admin.article.list')]);courses.value=result[0];articles.value=result[1]}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
function edit(c?:CourseEntity){editing.value=c?JSON.parse(JSON.stringify(c)):{id:'',title:'',summary:'',stage:'',variety:'',status:'DRAFT',sortOrder:0,version:0,articleIds:[]};error.value='';selected.value=''}
function add(){if(editing.value && selected.value && !editing.value.articleIds.includes(selected.value))editing.value.articleIds.push(selected.value);selected.value=''}
function move(index:number,delta:number){const ids=editing.value?.articleIds;if(!ids || index+delta<0 || index+delta>=ids.length)return;[ids[index],ids[index+delta]]=[ids[index+delta],ids[index]]}
async function save(){if(!editing.value || busy.value)return;busy.value=true;error.value='';try{await api('admin.course.save',{course:editing.value,expectedVersion:editing.value.version});editing.value=null;await load()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
onMounted(load)
</script>
<template>
<section class="panel">
 <div class="panel-head"><h3>课程编排</h3><button v-permission="'article.write'" class="button primary" :disabled="busy" @click="edit()">新建课程</button><button class="button secondary" :disabled="busy" @click="load">刷新</button></div>
 <p>课程独立管理，按课时顺序学习。同一内容可加入多门课程；下架课时自动从学员目录隐藏。</p>
 <p v-if="error" class="form-error" role="alert">{{error}}</p><p v-if="busy">正在处理…</p><p v-else-if="!courses.length">暂无课程，创建课程后添加课时。</p>
 <article v-for="c in courses" :key="c.id" class="panel"><h3>{{c.title}}</h3><p>{{c.summary}}</p><p>{{c.status==='PUBLISHED'?'已发布':'草稿'}} · {{c.articleIds.length}} 节 · 排序 {{c.sortOrder}}</p><button v-permission="'article.write'" class="button secondary" :disabled="busy" @click="edit(c)">编辑编排</button></article>
 <div v-if="editing" class="modal-backdrop"><section class="drawer" role="dialog" aria-modal="true" aria-label="课程编排"><div class="drawer-head"><h2>课程编排</h2><button :disabled="busy" @click="editing=null">关闭</button></div><form @submit.prevent="save"><div class="drawer-body">
 <label>课程标题<input v-model.trim="editing.title" required maxlength="120" :disabled="busy"/></label><label>课程简介<textarea v-model="editing.summary" maxlength="1000" :disabled="busy"/></label>
 <label>阶段<input v-model="editing.stage" maxlength="80" :disabled="busy"/></label><label>品种<input v-model="editing.variety" maxlength="80" :disabled="busy"/></label><label>排序（小值优先）<input v-model.number="editing.sortOrder" type="number" min="-100000" max="100000" required :disabled="busy"/></label>
 <label>状态<select v-model="editing.status" :disabled="busy"><option value="DRAFT">草稿</option><option value="PUBLISHED">已发布</option></select></label>
 <label>添加课时<select v-model="selected" :disabled="busy"><option value="">选择内容</option><option v-for="a in articles.filter(a=>!editing?.articleIds.includes(a.id))" :key="a.id" :value="a.id">{{a.title}} · {{a.status}}</option></select></label><button type="button" class="button secondary" :disabled="busy || !selected" @click="add">添加课时</button>
 <ol><li v-for="(id,index) in editing.articleIds" :key="id">{{articles.find(a=>a.id===id)?.title || '内容已移除'}} <button type="button" :disabled="busy || index===0" @click="move(index,-1)">上移</button><button type="button" :disabled="busy || index===editing.articleIds.length-1" @click="move(index,1)">下移</button><button type="button" :disabled="busy" @click="editing.articleIds.splice(index,1)">移除</button></li></ol>
 <p v-if="error" class="form-error" role="alert">{{error}}</p></div><div class="drawer-foot"><button v-permission="'article.write'" class="button primary" :disabled="busy">保存课程</button></div></form></section></div>
</section>
</template>
