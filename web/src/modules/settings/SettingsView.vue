<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../../components/Icon.vue'
import NotConnected from '../../components/NotConnected.vue'
import * as api from '../../api'
import { useConsole } from '../../console'
import { durationOf, num, relativeOf } from '../../format'
import type { JEVKey, Settings } from '../../types'

/**
 * 网关与安全策略配置。
 *
 * 表单字段与后端 `Settings` 一一对应，读写走既有 `GET/PUT /api/settings`；
 * 密钥池走既有 `/api/keys` 及其子路径。原型中出现、但现有后端没有对应字段的
 * 控件（连通性测试、密钥健康成功率、分发池负载、WORM 归档与校验码、
 * 权重轮询、永久封禁、心跳间隔）一律以降级态呈现，不提交无效字段、
 * 不显示任何无来源数值。
 */
const { settings, keys: keyPool, refresh, applySettings } = useConsole()

const form = ref<Settings>({ ...settings.value })
watch(
  settings,
  (s) => {
    form.value = { ...s }
  },
)

const dirty = computed(() => JSON.stringify(form.value) !== JSON.stringify(settings.value))

const saving = ref(false)
const saveError = ref('')
const saveOk = ref('')

// --- 校验：与 internal/config/store.go 的 UpdateSettings 保持一致 ---

const thresholdValid = computed(() => {
  const v = Number(form.value.safety_threshold)
  return Number.isFinite(v) && v >= 0 && v <= 1
})

const validationError = computed(() => {
  if (!thresholdValid.value) return '安全判定阈值必须是 0 到 1 之间的数值'
  if (!form.value.upstream_base_url.trim()) return '上游业务接口地址不能为空'
  if (!form.value.jev_base_url.trim()) return 'JEV 安全检定接口不能为空'
  return ''
})

/** 滑杆与当前值保持同步：滑杆步进 0.005，输入框可精确到三位小数。 */
const thresholdPct = computed({
  get: () => String(Math.min(1, Math.max(0, Number(form.value.safety_threshold) || 0))),
  set: (v: string) => {
    form.value.safety_threshold = Math.round(Number(v) * 1000) / 1000
  },
})

function onThresholdInput(e: Event): void {
  const v = Number((e.target as HTMLInputElement).value)
  if (Number.isFinite(v)) form.value.safety_threshold = v
}

const thresholdDisplay = computed(() => (Number(form.value.safety_threshold) || 0).toFixed(3))

async function save(): Promise<void> {
  saveError.value = ''
  saveOk.value = ''
  if (validationError.value) {
    saveError.value = validationError.value
    return
  }
  saving.value = true
  try {
    const updated = await api.saveSettings({ ...form.value, safety_threshold: Number(form.value.safety_threshold) || 0 })
    form.value = { ...updated }
    saveOk.value = '配置已保存并生效'
    await applySettings(updated)
    window.setTimeout(() => (saveOk.value = ''), 2500)
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

function reset(): void {
  form.value = { ...settings.value }
  saveError.value = ''
  saveOk.value = ''
}

/** 封禁时长预设。后端 `abuse_ban_sec` 是秒数，因此不提供「永久」预设。 */
const BAN_PRESETS = [
  { label: '10 分钟', sec: 600 },
  { label: '1 小时', sec: 3600 },
  { label: '24 小时', sec: 86400 },
]

// --- 密钥池 ---

const newLabel = ref('')
const newKey = ref('')
const keyBusy = ref(false)
const keyError = ref('')

async function addKey(): Promise<void> {
  keyError.value = ''
  const label = newLabel.value.trim()
  const key = newKey.value.trim()
  if (!key) {
    keyError.value = '请填写密钥内容'
    return
  }
  keyBusy.value = true
  try {
    await api.addKey(label || '未命名密钥', key)
    newLabel.value = ''
    newKey.value = ''
    void refresh()
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  } finally {
    keyBusy.value = false
  }
}

async function toggleKey(k: JEVKey): Promise<void> {
  keyError.value = ''
  keyBusy.value = true
  try {
    await api.setKeyEnabled(k.id, !k.enabled)
    void refresh()
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  } finally {
    keyBusy.value = false
  }
}

async function removeKey(k: JEVKey): Promise<void> {
  keyError.value = ''
  if (!window.confirm(`确认删除密钥「${k.label}」？该操作不可撤销。`)) return
  keyBusy.value = true
  try {
    await api.deleteKey(k.id)
    void refresh()
  } catch (e) {
    keyError.value = e instanceof Error ? e.message : String(e)
  } finally {
    keyBusy.value = false
  }
}

const keysEnabled = computed(() => keyPool.value.filter((k) => k.enabled).length)
const totalCalls = computed(() => keyPool.value.reduce((a, k) => a + (k.calls || 0), 0))
</script>

<template>
  <div class="w-full px-margin py-margin flex flex-col gap-space-lg">
    <!-- 页头 -->
    <div class="flex flex-col lg:flex-row lg:items-end justify-between gap-space-md">
      <div class="flex flex-col gap-1">
        <div class="flex items-center gap-space-xs flex-wrap">
          <h1 class="text-title-2 font-title-2 text-on-surface tracking-tight">网关与安全策略配置</h1>
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-surface-bright text-on-surface-variant">
            Security Matrix Console
          </span>
        </div>
        <p class="text-subheadline font-subheadline text-on-surface-variant">
          配置上游路由、检定阈值、自动拉黑策略与调用密钥池；保存后立即对代理主链路生效。
        </p>
      </div>
      <div class="flex items-center gap-space-sm">
        <span v-if="dirty" class="inline-flex items-center gap-1 text-caption-1 font-caption-1 text-tertiary">
          <span class="h-1.5 w-1.5 rounded-full bg-tertiary"></span>
          有未保存的修改
        </span>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all disabled:opacity-50"
          :disabled="saving || !dirty"
          @click="reset"
        >
          <Icon name="refresh" class="text-[16px]" />
          <span>放弃修改</span>
        </button>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-primary-container hover:bg-primary-container/90 text-on-primary font-subheadline font-subheadline font-semibold shadow-md transition-all active:scale-[0.98] disabled:opacity-60"
          :disabled="saving"
          @click="save"
        >
          <Icon :name="saving ? 'loader' : 'check-circle'" class="text-[16px]" :class="saving ? 'animate-spin' : ''" />
          <span>保存并热重载配置</span>
        </button>
      </div>
    </div>

    <!-- 保存反馈 -->
    <div
      v-if="saveError || saveOk"
      class="flex items-center gap-2 px-space-md py-2.5 rounded-xl border"
      :class="saveError ? 'bg-error-container/20 border-error/40 text-error' : 'bg-secondary/10 border-secondary/30 text-secondary'"
    >
      <Icon :name="saveError ? 'alert-circle' : 'check-circle'" class="text-[16px]" />
      <span class="text-subheadline font-subheadline">{{ saveError || saveOk }}</span>
    </div>

    <!-- 语义提示：阈值方向由 block_if_below 决定 -->
    <div class="flex items-start gap-2 px-space-md py-2.5 rounded-xl bg-surface-container border border-hairline">
      <Icon name="info" class="text-primary text-[16px] mt-0.5 shrink-0" />
      <span class="text-caption-1 font-caption-1 text-on-surface-variant leading-relaxed">
        判定语义：JEV 返回的分值越高表示越安全。当前
        <span class="font-code-badge text-code-badge text-primary mono">block_if_below = {{ form.block_if_below }}</span>
        ，因此<span class="text-on-surface">{{ form.block_if_below ? '分值低于阈值即拦截' : '分值达到或高于阈值即拦截' }}</span>。
        若改写「安全判定指令」为反向提问（例如「是否有害」），需要同时翻转该开关。
      </span>
    </div>

    <!-- 分组 1：上游服务与 JEV 检定路由 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-lg py-space-md border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <div class="w-9 h-9 rounded-xl bg-primary/10 flex items-center justify-center text-primary">
            <Icon name="link" class="text-[18px]" />
          </div>
          <div class="flex flex-col">
            <span class="text-headline font-headline text-on-surface">上游服务与 JEV 检定路由</span>
            <span class="text-caption-2 font-caption-2 text-outline">Upstream Routing &amp; Inspection Core</span>
          </div>
        </div>
        <NotConnected reason="后端未提供连通性探测接口，无法在此发起测试。" />
      </div>

      <div class="px-space-lg py-space-md grid grid-cols-1 lg:grid-cols-2 gap-space-md">
        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">上游业务接口地址</span>
          <input
            v-model.trim="form.upstream_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://api.example.com"
          />
          <span class="text-caption-2 font-caption-2 text-outline">安全请求将被反向代理转发到此地址。</span>
        </label>

        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">JEV 安全检定接口</span>
          <input
            v-model.trim="form.jev_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://jev.example.com"
          />
          <span class="text-caption-2 font-caption-2 text-outline">请求路径为 <span class="mono">/v1/systemone</span>。</span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">JEV 检定模型</span>
          <input
            v-model.trim="form.jev_model"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="jev-latest"
          />
          <span class="text-caption-2 font-caption-2 text-outline">留空时后端回退为 <span class="mono">jev-latest</span>。</span>
        </label>

        <div class="flex flex-col gap-1.5">
          <div class="flex items-center justify-between">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">安全判定阈值</span>
            <span
              class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge"
              :class="thresholdValid ? 'bg-primary/15 text-primary' : 'bg-error-container text-error'"
            >
              当前值 {{ thresholdDisplay }}
            </span>
          </div>
          <div class="flex items-center gap-space-sm">
            <input
              v-model="thresholdPct"
              type="range"
              min="0"
              max="1"
              step="0.005"
              class="flex-1 accent-[#4b8eff]"
            />
            <input
              :value="form.safety_threshold"
              type="number"
              min="0"
              max="1"
              step="0.001"
              class="w-24 px-2 py-1.5 rounded-lg bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest shadow-inset"
              @input="onThresholdInput"
            />
          </div>
          <span class="text-caption-2 font-caption-2" :class="thresholdValid ? 'text-outline' : 'text-error'">
            取值范围 0 ~ 1，与后端校验一致；超出范围将无法保存。
          </span>
        </div>

        <label class="flex flex-col gap-1.5 lg:col-span-2">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">安全判定指令</span>
          <textarea
            v-model="form.safety_instruction"
            rows="4"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
          <span class="text-caption-2 font-caption-2 text-outline">
            发送给检定模型的提问。分值为 0 ~ 1 的概率，方向由上方「低于阈值即拦截」开关决定。
          </span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">送检字符上限</span>
          <input
            v-model.number="form.max_state_chars"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">按字符截断送检内容；填 0 或负数时后端回退为 16000。</span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">检定超时阈值（毫秒）</span>
          <input
            v-model.number="form.jev_timeout_ms"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">超时后的行为由「JEV 异常放行」开关决定；填 0 或负数时后端回退为 8000。</span>
        </label>
      </div>
    </section>

    <!-- 分组 2：安全防护与自动拉黑策略 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-lg py-space-md border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <div class="w-9 h-9 rounded-xl bg-error-container/40 flex items-center justify-center text-error">
            <Icon name="ban" class="text-[18px]" />
          </div>
          <div class="flex flex-col">
            <span class="text-headline font-headline text-on-surface">安全防护与攻击自动拉黑策略</span>
            <span class="text-caption-2 font-caption-2 text-outline">Abuse Shield &amp; Auto-Ban Policy</span>
          </div>
        </div>
        <label class="flex items-center gap-2 cursor-pointer">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ form.abuse_enabled ? '已开启' : '已关闭' }}</span>
          <span class="relative inline-flex items-center">
            <input v-model="form.abuse_enabled" class="sr-only peer" type="checkbox" />
            <span
              class="w-11 h-6 bg-surface-container-highest rounded-full peer peer-checked:bg-secondary-container peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-5 after:w-5 after:transition-all shadow-sm"
            ></span>
          </span>
        </label>
      </div>

      <div class="px-space-lg py-space-md flex flex-col gap-space-md">
        <div class="grid grid-cols-1 md:grid-cols-3 gap-space-md">
          <label class="flex flex-col gap-1.5">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">周期统计窗口（秒）</span>
            <input
              v-model.number="form.abuse_window_sec"
              type="number"
              min="1"
              :disabled="!form.abuse_enabled"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
            />
            <span class="text-caption-2 font-caption-2 text-outline">滑动窗口长度；≤0 时后端回退为 60。</span>
          </label>

          <label class="flex flex-col gap-1.5">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">违规攻击次数阈值（次）</span>
            <input
              v-model.number="form.abuse_max_harmful"
              type="number"
              min="1"
              :disabled="!form.abuse_enabled"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
            />
            <span class="text-caption-2 font-caption-2 text-outline">窗口内达到该次数即拉黑来源 IP；≤0 时回退为 5。</span>
          </label>

          <div class="flex flex-col gap-1.5">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">自动封禁时长（秒）</span>
            <input
              v-model.number="form.abuse_ban_sec"
              type="number"
              min="1"
              :disabled="!form.abuse_enabled"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
            />
            <div class="flex items-center gap-space-xs flex-wrap">
              <button
                v-for="p in BAN_PRESETS"
                :key="p.sec"
                type="button"
                class="px-2 py-0.5 rounded-md bg-surface-container-high hover:bg-surface-container-highest text-caption-2 font-caption-2 text-on-surface-variant transition-colors"
                :disabled="!form.abuse_enabled"
                @click="form.abuse_ban_sec = p.sec"
              >
                {{ p.label }}
              </button>
              <span class="text-caption-2 font-caption-2 text-outline">当前 {{ durationOf(form.abuse_ban_sec || 0) }}</span>
            </div>
            <span class="text-caption-2 font-caption-2 text-outline">
              <NotConnected reason="后端以秒数记录封禁时长，没有「永久封禁」语义，因此不提供该预设。" /> 分钟/小时/天级预设见上；≤0 时回退为 300。
            </span>
          </div>
        </div>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">封禁后自定义提示语</span>
          <textarea
            v-model="form.block_message"
            rows="3"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-code-body font-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
          <span class="text-caption-2 font-caption-2 text-outline">内容拦截与滥用封禁时返回给客户端的提示正文。</span>
        </label>

        <div class="flex flex-col gap-space-sm p-space-md rounded-xl bg-surface-container-low/70 shadow-inset">
          <div class="flex items-center justify-between flex-wrap gap-space-xs">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">拦截响应状态码</span>
            <div class="flex items-center gap-space-md text-caption-1 font-caption-1">
              <span class="flex items-center gap-1.5">
                <span class="px-1.5 py-0.5 rounded bg-error-container text-error font-code-badge text-code-badge mono">403</span>
                <span class="text-on-surface-variant">内容拦截 · <span class="mono">X-JEV-Gateway: blocked</span></span>
              </span>
              <span class="flex items-center gap-1.5">
                <span class="px-1.5 py-0.5 rounded bg-tertiary-container/40 text-tertiary font-code-badge text-code-badge mono">429</span>
                <span class="text-on-surface-variant">滥用封禁 · <span class="mono">X-JEV-Gateway: banned</span></span>
              </span>
            </div>
          </div>
          <span class="text-caption-2 font-caption-2 text-outline">
            状态码由代理主链路固定返回，不是可配置项（上表为当前实现的真实取值）；因此原型中的状态码输入框改为只读展示。
          </span>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-space-sm">
          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.check_response" type="checkbox" class="mt-0.5 h-4 w-4 accent-[#4b8eff]" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">同时审核上游输出响应</span>
              <span class="text-caption-2 font-caption-2 text-outline">开启后会缓冲整个上游响应以审核，<span class="text-tertiary">会中断 SSE 流式输出</span>，默认关闭。</span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.block_if_below" type="checkbox" class="mt-0.5 h-4 w-4 accent-[#4b8eff]" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">低于安全阈值即刻阻断</span>
              <span class="text-caption-2 font-caption-2 text-outline">开启时分值 &lt; 阈值判为拦截；关闭时改为分值 ≥ 阈值判为拦截。</span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.fail_open" type="checkbox" class="mt-0.5 h-4 w-4 accent-[#4b8eff]" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">JEV 异常时放行（Fail-Open）</span>
              <span class="text-caption-2 font-caption-2 text-outline">开启时 JEV 不可达则照常转发；关闭时改为阻断（Fail-Close）。</span>
            </span>
          </label>
        </div>
      </div>
    </section>

    <!-- 分组 3：调用密钥池分发 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-lg py-space-md border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <div class="w-9 h-9 rounded-xl bg-secondary/10 flex items-center justify-center text-secondary">
            <Icon name="key" class="text-[18px]" />
          </div>
          <div class="flex flex-col">
            <span class="text-headline font-headline text-on-surface">调用密钥池分发</span>
            <span class="text-caption-2 font-caption-2 text-outline">Multi-Key Dispatch Pool</span>
          </div>
        </div>
        <div class="flex items-center gap-space-xs flex-wrap">
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
            轮询分发 (Round-Robin)
          </span>
          <NotConnected reason="后端按启用顺序原子轮询，没有权重配置与负载统计。" />
        </div>
      </div>

      <div class="px-space-lg py-space-md flex flex-col gap-space-md">
        <div class="flex items-end gap-space-sm flex-wrap">
          <label class="flex flex-col gap-1.5 flex-1 min-w-[10rem]">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">密钥标签</span>
            <input
              v-model="newLabel"
              type="text"
              placeholder="例如 Primary-Alpha-01"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            />
          </label>
          <label class="flex flex-col gap-1.5 flex-[2] min-w-[14rem]">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">密钥内容</span>
            <input
              v-model="newKey"
              type="password"
              autocomplete="off"
              placeholder="粘贴 JEV API Key"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
              @keydown.enter.prevent="addKey"
            />
          </label>
          <button
            type="button"
            class="inline-flex items-center gap-1.5 px-4 py-2.5 rounded-xl bg-primary-container hover:bg-primary-container/90 text-on-primary text-subheadline font-subheadline font-semibold shadow-md transition-all active:scale-[0.98] disabled:opacity-60"
            :disabled="keyBusy"
            @click="addKey"
          >
            <Icon :name="keyBusy ? 'loader' : 'key'" class="text-[16px]" :class="keyBusy ? 'animate-spin' : ''" />
            <span>添加入池</span>
          </button>
        </div>

        <p v-if="keyError" class="text-caption-1 font-caption-1 text-error">{{ keyError }}</p>

        <div class="flex flex-col divide-y divide-hairline rounded-xl bg-surface-container-low/50 overflow-hidden">
          <div
            v-for="k in keyPool"
            :key="k.id"
            class="flex items-center justify-between gap-space-sm px-space-md py-space-sm flex-wrap"
          >
            <div class="flex items-center gap-space-sm min-w-[12rem]">
              <span class="h-2 w-2 rounded-full shrink-0" :class="k.enabled ? 'bg-secondary' : 'bg-outline'"></span>
              <div class="flex flex-col">
                <span class="text-subheadline font-subheadline text-on-surface">{{ k.label }}</span>
                <span class="text-caption-2 font-caption-2 text-outline font-code-body mono">{{ k.masked }}</span>
              </div>
            </div>

            <div class="flex items-center gap-space-md flex-wrap">
              <div class="flex flex-col">
                <span class="text-caption-1 font-caption-1 text-on-surface mono">{{ num(k.calls) }} 次</span>
                <span class="text-caption-2 font-caption-2 text-outline">最近使用 {{ relativeOf(k.last_used) }}</span>
              </div>
              <NotConnected reason="后端不记录调用成功/失败结果，无法给出成功率。" />
              <label class="relative inline-flex items-center cursor-pointer" :title="k.enabled ? '停用该密钥' : '启用该密钥'">
                <input
                  class="sr-only peer"
                  type="checkbox"
                  :checked="k.enabled"
                  :disabled="keyBusy"
                  @change="toggleKey(k)"
                />
                <span
                  class="w-10 h-5 bg-surface-container-highest rounded-full peer peer-checked:bg-secondary-container peer-checked:after:translate-x-full after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:rounded-full after:h-4 after:w-4 after:transition-all shadow-sm"
                ></span>
              </label>
              <button
                type="button"
                class="inline-flex items-center gap-1 px-2 py-1 rounded-lg text-caption-1 font-caption-1 text-on-surface-variant hover:bg-error-container/30 hover:text-error transition-colors"
                :disabled="keyBusy"
                @click="removeKey(k)"
              >
                <Icon name="x" class="text-[14px]" />
                <span>删除</span>
              </button>
            </div>
          </div>

          <div v-if="!keyPool.length" class="px-space-md py-space-lg flex flex-col items-center gap-1.5">
            <Icon name="key" class="text-outline text-[22px]" />
            <span class="text-subheadline font-subheadline text-on-surface-variant">密钥池为空</span>
            <span class="text-caption-2 font-caption-2 text-outline">未配置密钥时，检定请求将没有可用凭据。</span>
          </div>
        </div>

        <div class="grid grid-cols-2 md:grid-cols-4 gap-space-sm">
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">启用密钥</span>
            <span class="text-headline font-headline text-on-surface mono">{{ keysEnabled }} / {{ keyPool.length }}</span>
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">累计调用</span>
            <span class="text-headline font-headline text-on-surface mono">{{ num(totalCalls) }}</span>
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">健康心跳</span>
            <NotConnected reason="后端不做主动健康探测，只在真实请求 401/429/529 时切换到下一把密钥。" />
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">分发池负载</span>
            <NotConnected reason="后端无按池聚合的吞吐/负载指标。" />
          </div>
        </div>

        <div class="flex flex-col gap-space-xs p-space-md rounded-xl border border-dashed border-outline-variant/60">
          <div class="flex items-center justify-between flex-wrap gap-space-xs">
            <span class="text-subheadline font-subheadline text-on-surface">合规基准与审计归档</span>
            <NotConnected reason="后端无 WORM 归档、校验码与合规基准能力；审计数据存放在 SQLite 的 logs 表中，可由运维侧自行备份。" />
          </div>
          <span class="text-caption-2 font-caption-2 text-outline">
            转发审计记录视图展示的原始痕迹即当前可用的全部审计能力。
          </span>
        </div>
      </div>
    </section>
  </div>
</template>
