<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterView, useRoute, useRouter } from 'vue-router'
import AppShell from './components/AppShell.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import LoginView from './components/LoginView.vue'
import * as api from './api'
import { provideConsole } from './console'
import { t, useI18n } from './i18n'
import type { Settings, StateResponse } from './types'

/**
 * 控制台根组件：鉴权闸门 + 路由出口。
 *
 * 只使用既有接口：`GET /api/setup-status` 决定登录视图还是初始配置视图，
 * `GET /api/state` 一次性取回配置、密钥池与 24h 统计，再分发给当前路由界面。
 *
 * 三个业务界面是独立的路由页面（见 `modules/`），由 `router-view` 渲染，
 * 壳层不参与界面选择；认证入口不占路由，因此未登录时 URL 仍是用户要访问的界面，
 * 登录成功后直接落到该界面。
 */
const booted = ref(false)
const needsSetup = ref(false)
const authed = ref(false)
const bootError = ref('')
const state = ref<StateResponse | null>(null)
const busy = ref(false)

const route = useRoute()
const router = useRouter()

/** 会话失效（任意受保护接口返回 401）时回到登录视图。 */
api.setUnauthorizedHandler(() => {
  authed.value = false
  state.value = null
})

async function boot(): Promise<void> {
  bootError.value = ''
  try {
    const s = await api.setupStatus()
    needsSetup.value = s.needs_setup
  } catch (e) {
    booted.value = true
    bootError.value = e instanceof Error ? e.message : String(e)
    return
  }

  if (!needsSetup.value && api.hasToken()) {
    try {
      state.value = await api.getState()
      authed.value = true
    } catch {
      // token 失效或网络异常：退回登录视图，具体原因由登录视图的报错行呈现。
      authed.value = false
      state.value = null
    }
  } else {
    authed.value = false
  }
  booted.value = true
}

async function refresh(): Promise<void> {
  try {
    state.value = await api.getState()
  } catch {
    /* 刷新失败保持现有数据，不清空界面 */
  }
}

onMounted(boot)

async function onAuthenticated(): Promise<void> {
  await boot()
}

async function onLogout(): Promise<void> {
  await api.logout()
  authed.value = false
  state.value = null
  needsSetup.value = false
  await router.push({ name: 'dashboard' })
  void api.setupStatus().then((s) => (needsSetup.value = s.needs_setup)).catch(() => undefined)
}

/** 顶部状态条的总开关：直接写回 `enabled` 字段。 */
async function onToggleEnabled(next: boolean): Promise<void> {
  if (!state.value || busy.value) return
  busy.value = true
  try {
    const updated = await api.saveSettings({ ...state.value.settings, enabled: next })
    state.value.settings = updated
    await refresh()
  } catch {
    await refresh()
  } finally {
    busy.value = false
  }
}

/** 配置视图保存成功后同步壳层与全局状态。 */
async function onSettingsSaved(updated: Settings): Promise<void> {
  if (state.value) state.value.settings = updated
  await refresh()
}

/**
 * 浏览器标签页标题。未登录时显示认证入口的标题，登录后跟随当前路由界面，
 * 避免停留在登录页却显示上一次界面的标题。界面标题按当前语言取用。
 */
const { locale } = useI18n()

function syncTitle(): void {
  if (!authed.value) {
    document.title = needsSetup.value ? t('title.setup') : t('title.login')
    return
  }
  const meta = route.meta.title as { zh?: string; en?: string } | undefined
  const screen = locale.value === 'en' ? meta?.en : meta?.zh
  document.title = screen ? `${screen} · ${t('app.name')}` : t('app.name')
}

router.afterEach(syncTitle)
watch([authed, needsSetup, locale], syncTitle, { immediate: true })

/**
 * 装载共享上下文。此处按需求值：路由界面只在 `state` 就绪（`AppShell` 已渲染）
 * 之后才会读取，因此不会取到空值。
 */
provideConsole({
  settings: computed(() => state.value!.settings),
  keys: computed(() => state.value!.keys),
  stats24h: computed(() => state.value!.stats24h),
  refresh,
  applySettings: onSettingsSaved,
})
</script>

<template>
  <div v-if="!booted" class="min-h-screen w-full flex items-center justify-center bg-surface">
    <div class="flex flex-col items-center gap-space-sm">
      <div class="h-10 w-10 rounded-xl bg-surface-container-high flex items-center justify-center">
        <span class="h-2.5 w-2.5 rounded-full bg-primary animate-pulse"></span>
      </div>
      <span class="text-caption-1 font-caption-1 text-outline">{{ t('boot.connecting') }}</span>
    </div>
  </div>

  <LoginView
    v-else-if="!authed"
    :needs-setup="needsSetup"
    :initial-error="bootError"
    @authenticated="onAuthenticated"
  />

  <AppShell
    v-else-if="state"
    :enabled="state.settings.enabled"
    :jev-model="state.settings.jev_model"
    :keys-enabled="state.keys.filter((k) => k.enabled).length"
    :keys-total="state.keys.length"
    :busy="busy"
    @logout="onLogout"
    @toggle-enabled="onToggleEnabled"
  >
    <RouterView />
  </AppShell>

  <!-- 确认弹框宿主：全局唯一一个，由 confirmDialog() 驱动；挂在这里而不是各页面内，
       是为了让任何位置的调用都用同一个弹框，且不受页面容器的层级与裁剪影响。 -->
  <ConfirmDialog />
</template>
