<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import Icon from './Icon.vue'
import NotConnected from './NotConnected.vue'
import * as api from '../api'

/**
 * 登录 / 初始配置视图（原型 `jev_3`）。
 *
 * 结构按原型还原：居中 440px 玻璃认证卡与背景环境光晕、App 图标与渐变光环、
 * 居中标题与副标题、状态胶囊、口令输入行（左置图标 + 右置可见性切换 + 右侧元信息）、
 * 带箭头的主按钮、守护进程状态胶囊、安全标语与页脚；提交结果以浮动提示呈现。
 *
 * 与后端事实冲突的元素按「分类处理」口径处理（见 docs/admin-console-backend-gaps.md）：
 * - `ZERO-TRUST TLS`、`PII SHIELD ACTIVE` 是静态标语，原样保留；
 * - 版本徽标（原型 `v2.4.1-rc3`）、延迟徽标（原型 `LATENCY <1.8ms`）、
 *   守护进程地址胶囊（原型 `127.0.0.1:8080`）没有接口来源，改以「未接入」降级态呈现，
 *   不显示任何伪造数值；
 * - 口令哈希元信息标注为后端真实使用的 bcrypt；
 * - 不呈现「保持本工作站受信任凭据 (24h)」勾选项，也不提供语言切换。
 *
 * 登录与初始设置的行为与既有接口语义保持一致：`GET /api/setup-status` 决定模式、
 * 初始设置校验口令长度与两次输入一致性、`POST /api/setup` / `POST /api/login`
 * 返回 token。另一模式在本界面中呈禁用态，避免提交到与后端状态不符的接口。
 */
const props = defineProps<{ needsSetup: boolean; initialError?: string }>()
const emit = defineEmits<{ (e: 'authenticated'): void }>()

const pw = ref('')
const pw2 = ref('')
const busy = ref(false)
const showPw = ref(false)
const showPw2 = ref(false)

/** 浮动提示：错误保持到下次提交，成功提示在跳转前短暂呈现。 */
const toast = ref<{ text: string; ok: boolean } | null>(null)
let toastTimer: number | undefined

function showToast(text: string, ok: boolean): void {
  if (toastTimer !== undefined) window.clearTimeout(toastTimer)
  toast.value = { text, ok }
  if (ok) {
    toastTimer = window.setTimeout(() => (toast.value = null), 2400)
  }
}

if (props.initialError) showToast(props.initialError, false)

const pwInput = ref<HTMLInputElement | null>(null)
const pw2Input = ref<HTMLInputElement | null>(null)

onMounted(() => pwInput.value?.focus())

const setupMode = computed(() => props.needsSetup)

const title = 'JEV 安全网关'
const subtitle = 'AI Request Security & Filtering Gateway'
const statusText = computed(() =>
  setupMode.value ? '首次使用：请设置管理员口令（至少 6 位）' : '输入管理员口令以访问控制台',
)
/** 口令哈希元信息：后端使用 bcrypt（见 internal/admin/auth.go），不是原型标注的 SHA-256。 */
const passMeta = computed(() => (setupMode.value ? 'bcrypt · 至少 6 位' : 'bcrypt'))

async function submit(): Promise<void> {
  const password = pw.value
  busy.value = true
  try {
    if (setupMode.value) {
      if (password.length < 6) throw new Error('口令至少 6 位')
      if (password !== pw2.value) throw new Error('两次输入不一致')
      await api.setup(password)
      showToast('网关密钥已初始化并激活', true)
    } else {
      await api.login(password)
      showToast('验证通过，正在载入安全控制台…', true)
    }
    pw.value = ''
    pw2.value = ''
    emit('authenticated')
  } catch (e) {
    showToast(e instanceof Error ? e.message : String(e), false)
  } finally {
    busy.value = false
  }
}

async function onPwEnter(): Promise<void> {
  if (setupMode.value) {
    await nextTick()
    pw2Input.value?.focus()
    return
  }
  await submit()
}
</script>

<template>
  <main class="min-h-screen w-full flex items-center justify-center p-space-md bg-surface">
    <div class="flex flex-col w-full items-center justify-center relative py-space-xl">
      <!-- 卡片背后的环境光晕（纯装饰，无数据含义） -->
      <div class="absolute w-96 h-96 rounded-full bg-primary-container/10 blur-3xl pointer-events-none -top-16 -left-16"></div>
      <div class="absolute w-80 h-80 rounded-full bg-secondary-container/10 blur-3xl pointer-events-none -bottom-10 -right-10"></div>

      <!-- 顶部工具条：版本徽标 + 认证模式分段控件 -->
      <div class="w-full max-w-[440px] flex items-center justify-between gap-space-sm mb-space-lg px-space-xs z-10">
        <div class="flex items-center gap-space-xs text-on-surface-variant min-w-0">
          <Icon name="verified-user" class="text-[18px] shrink-0" />
          <!-- 原型此处为硬编码版本号 v2.4.1-rc3；后端无版本接口，降级呈现 -->
          <NotConnected
            title="版本未接入"
            reason="后端没有版本/构建信息接口（缺口 G23），控制台无法读取自身版本号。"
          />
        </div>

        <div class="flex items-center bg-surface-container-high/80 p-0.5 rounded-full shadow-sm backdrop-blur-md shrink-0">
          <button
            type="button"
            class="font-caption-2 text-caption-2 px-space-sm py-0.5 rounded-full transition-all duration-200 flex items-center gap-1"
            :class="
              !setupMode
                ? 'bg-surface-bright text-on-surface shadow-sm'
                : 'text-on-surface-variant/40 cursor-not-allowed'
            "
            :disabled="setupMode"
            :aria-pressed="!setupMode"
          >
            <Icon name="login" class="text-[13px]" />
            <span>验证登录</span>
          </button>
          <button
            type="button"
            class="font-caption-2 text-caption-2 px-space-sm py-0.5 rounded-full transition-all duration-200 flex items-center gap-1"
            :class="
              setupMode
                ? 'bg-surface-bright text-on-surface shadow-sm'
                : 'text-on-surface-variant/40 cursor-not-allowed'
            "
            :disabled="!setupMode"
            :aria-pressed="setupMode"
          >
            <Icon name="key" class="text-[13px]" />
            <span>初始配置</span>
          </button>
        </div>
      </div>

      <!-- 认证卡（Cupertino Elevation 2 玻璃材质） -->
      <div
        class="w-full max-w-[440px] bg-surface-container/85 backdrop-blur-2xl rounded-xl shadow-2xl p-space-lg sm:p-space-xl flex flex-col items-center relative z-10 transition-all duration-300"
      >
        <!-- App 图标与渐变光环（原型引用公网图片，离线约束下改为本地内联图标） -->
        <div class="relative mb-space-md group">
          <div
            class="absolute -inset-1 rounded-xl bg-gradient-to-b from-primary-container to-inverse-primary opacity-40 blur-lg group-hover:opacity-70 transition duration-500"
          ></div>
          <div class="relative w-20 h-20 rounded-xl shadow-xl bg-surface-container-lowest flex items-center justify-center">
            <Icon name="shield-check" class="text-primary text-[40px]" />
          </div>
        </div>

        <!-- 标题与副标题 -->
        <div class="text-center mb-space-lg">
          <h1 class="font-title-2 text-title-2 text-on-surface tracking-tight">{{ title }}</h1>
          <p class="font-subheadline text-subheadline text-on-surface-variant mt-1">{{ subtitle }}</p>
        </div>

        <!-- 状态胶囊：当前模式来自 GET /api/setup-status -->
        <div
          class="w-full mb-space-lg py-1.5 px-space-md rounded-lg flex items-center justify-center gap-space-xs transition-colors duration-200"
          :class="setupMode ? 'bg-tertiary-container/20 text-tertiary' : 'bg-primary/10 text-primary'"
        >
          <Icon :name="setupMode ? 'key' : 'lock'" class="text-[16px] shrink-0" />
          <span class="font-caption-1 text-caption-1 font-medium tracking-tight text-center">{{ statusText }}</span>
        </div>

        <form class="w-full flex flex-col gap-space-md" @submit.prevent="submit">
          <!-- 口令输入行 -->
          <div class="flex flex-col gap-1.5">
            <div class="flex justify-between items-center px-1">
              <label class="font-caption-1 text-caption-1 text-on-surface-variant font-medium" for="adminPass">
                管理员主口令
              </label>
              <span class="font-code-badge text-code-badge text-on-surface-variant/70">{{ passMeta }}</span>
            </div>
            <div
              class="relative flex items-center bg-surface-container-high rounded-lg transition-all focus-within:bg-surface-container-highest shadow-inner"
            >
              <Icon name="shield-lock" class="text-outline absolute left-3 text-[19px] pointer-events-none" />
              <input
                id="adminPass"
                ref="pwInput"
                v-model="pw"
                :type="showPw ? 'text' : 'password'"
                autocomplete="current-password"
                class="w-full bg-transparent pl-10 pr-10 py-2.5 font-body text-body text-on-surface placeholder:text-outline-variant/60 focus:outline-none"
                placeholder="••••••••"
                required
                @keydown.enter.prevent="onPwEnter"
              />
              <button
                type="button"
                class="absolute right-2.5 text-outline hover:text-on-surface transition-colors p-1 flex items-center justify-center"
                :title="showPw ? '隐藏口令' : '显示口令'"
                :aria-label="showPw ? '隐藏口令' : '显示口令'"
                tabindex="-1"
                @click="showPw = !showPw"
              >
                <Icon :name="showPw ? 'eye' : 'eye-off'" class="text-[18px]" />
              </button>
            </div>
          </div>

          <!-- 确认口令行：仅初始配置模式 -->
          <div v-if="setupMode" class="flex flex-col gap-1.5">
            <div class="flex justify-between items-center px-1">
              <label class="font-caption-1 text-caption-1 text-on-surface-variant font-medium" for="confirmPass">
                重复确认新口令
              </label>
              <span class="font-code-badge text-code-badge text-tertiary">至少 6 位字符</span>
            </div>
            <div
              class="relative flex items-center bg-surface-container-high rounded-lg transition-all focus-within:bg-surface-container-highest shadow-inner"
            >
              <Icon name="pin" class="text-outline absolute left-3 text-[19px] pointer-events-none" />
              <input
                id="confirmPass"
                ref="pw2Input"
                v-model="pw2"
                :type="showPw2 ? 'text' : 'password'"
                autocomplete="new-password"
                class="w-full bg-transparent pl-10 pr-10 py-2.5 font-body text-body text-on-surface placeholder:text-outline-variant/60 focus:outline-none"
                placeholder="••••••••"
                @keydown.enter.prevent="submit"
              />
              <button
                type="button"
                class="absolute right-2.5 text-outline hover:text-on-surface transition-colors p-1 flex items-center justify-center"
                :title="showPw2 ? '隐藏口令' : '显示口令'"
                :aria-label="showPw2 ? '隐藏口令' : '显示口令'"
                tabindex="-1"
                @click="showPw2 = !showPw2"
              >
                <Icon :name="showPw2 ? 'eye' : 'eye-off'" class="text-[18px]" />
              </button>
            </div>
          </div>

          <!-- 主按钮 -->
          <button
            type="submit"
            class="w-full mt-space-sm bg-primary-container text-on-primary font-headline text-headline py-2.5 px-space-md rounded-lg shadow-md hover:opacity-95 active:scale-[0.99] transition duration-150 flex items-center justify-center gap-space-xs font-semibold disabled:opacity-60"
            :disabled="busy"
          >
            <span>{{ busy ? '处理中…' : setupMode ? '完成设置并进入' : '登录控制台' }}</span>
            <Icon name="arrow-right" class="text-[19px]" />
          </button>
        </form>

        <!-- 守护进程状态胶囊：地址与延迟都没有接口来源，此处只呈现可达性事实 -->
        <div class="w-full mt-space-lg pt-space-md flex flex-col items-center gap-space-xs">
          <div class="flex items-center gap-2 px-space-md py-1 rounded-full bg-surface-container-high/90 text-on-surface shadow-sm">
            <span class="w-2 h-2 rounded-full bg-secondary animate-pulse shrink-0"></span>
            <span class="font-code-badge text-code-badge tracking-tight text-on-surface-variant">
              管理服务已连接
            </span>
            <!-- 原型此处为硬编码守护进程地址 127.0.0.1:8080，无接口来源，降级呈现 -->
            <NotConnected
              title="地址未接入"
              reason="后端没有暴露守护进程监听地址或运行位置的接口，且管理口默认只监听 127.0.0.1。"
            />
          </div>

          <!-- 安全标语：静态标语原样保留；延迟徽标无数据来源，降级呈现 -->
          <div class="w-full flex items-center justify-between pt-space-sm text-on-surface-variant/60 font-code-badge text-[10px]">
            <span class="flex items-center gap-1">
              <Icon name="memory" class="text-[13px]" />
              <NotConnected title="延迟未接入" reason="后端没有端到端延迟的实时遥测接口（缺口 G2）。" />
            </span>
            <span class="flex items-center gap-1">
              <Icon name="encrypted" class="text-[13px] text-primary" />
              ZERO-TRUST TLS
            </span>
            <span class="flex items-center gap-1">
              <Icon name="rule" class="text-[13px] text-tertiary" />
              PII SHIELD ACTIVE
            </span>
          </div>
        </div>

        <div class="mt-space-lg text-center font-caption-2 text-caption-2 text-on-surface-variant/60">
          Typesafe AI / JEV Core Protected
        </div>
      </div>

      <!-- 浮动提示：提交结果 -->
      <div
        v-if="toast"
        class="fixed bottom-8 left-1/2 -translate-x-1/2 z-50 backdrop-blur-xl font-subheadline text-subheadline px-space-lg py-2.5 rounded-full shadow-2xl flex items-center gap-2 transition-all max-w-[90vw]"
        :class="toast.ok ? 'bg-surface-container-highest/95 text-on-surface' : 'bg-error-container/95 text-on-error-container'"
        role="status"
        aria-live="polite"
      >
        <Icon :name="toast.ok ? 'check-circle' : 'alert-circle'" class="text-[18px] shrink-0" :class="toast.ok ? 'text-secondary' : 'text-error'" />
        <span>{{ toast.text }}</span>
      </div>
    </div>
  </main>
</template>
