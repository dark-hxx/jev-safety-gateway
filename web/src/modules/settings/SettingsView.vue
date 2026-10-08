<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../../components/Icon.vue'
import HintTip from '../../components/HintTip.vue'
import * as api from '../../api'
import { confirmDialog } from '../../confirm'
import { useConsole } from '../../console'
import { durationOf, num, pctText, relativeOf } from '../../format'
import { useI18n } from '../../i18n'
import type { JEVKey, Settings } from '../../types'

/**
 * 网关与安全策略配置。
 *
 * 表单字段与后端 `Settings` 一一对应，读写走既有 `GET/PUT /api/settings`；
 * 密钥池走既有 `/api/keys` 及其子路径。
 *
 * 密钥健康来自 `jev_keys` 的 `ok_calls` / `err_calls` / `last_error`：每次检定尝试
 * 都记在当时使用的那把密钥上（包括可重试的 401/429/529 与网络失败），所以在真实
 * 请求中失败过的密钥会被标出来，不需要额外的主动探测接口。
 *
 * 原型中出现、但现有后端没有对应字段的控件（连通性测试、密钥权重、心跳间隔、
 * 分发池负载、WORM 归档与校验码）一律不再呈现：它们没有后端来源，留着只会是
 * 永久降级态。合规归档那一条保留为一句说明——审计数据确实落在 SQLite 的 logs
 * 表里，可由运维自行备份，这不是缺失的能力。
 */
const { settings, keys: keyPool, refresh, applySettings } = useConsole()
const { t } = useI18n()

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
  if (!thresholdValid.value) return t('settings.err.threshold')
  if (!form.value.upstream_base_url.trim()) return t('settings.err.upstream')
  if (!form.value.jev_base_url.trim()) return t('settings.err.jev')
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
    saveOk.value = t('settings.saved')
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

/** 封禁时长预设。后端 `abuse_ban_sec` 是秒数，永久封禁由 `abuse_ban_permanent` 单独开关。 */
const BAN_PRESETS = [
  { labelKey: 'settings.banPreset10m', sec: 600 },
  { labelKey: 'settings.banPreset1h', sec: 3600 },
  { labelKey: 'settings.banPreset24h', sec: 86400 },
] as const

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
    keyError.value = t('settings.addKeyError')
    return
  }
  keyBusy.value = true
  try {
    await api.addKey(label || t('settings.unnamedKey'), key)
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
  if (
    !(await confirmDialog({
      title: t('settings.deleteKeyTitle'),
      message: t('settings.deleteKeyConfirm', { label: k.label }),
      confirmText: t('settings.delete'),
      danger: true,
    }))
  )
    return
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
const okCalls = computed(() => keyPool.value.reduce((a, k) => a + (k.ok_calls || 0), 0))
const errCalls = computed(() => keyPool.value.reduce((a, k) => a + (k.err_calls || 0), 0))

/** 池整体的成功率；一次调用都没有时为 null，此时显示「—」而不是 0%。 */
const successRate = computed(() => pctText(okCalls.value, totalCalls.value))

/**
 * 单把密钥的成功率。`calls` 是尝试次数口径，所以分母就是它；没有调用记录时
 * 返回 null，由模板显示「—」——「从未用过」和「0% 成功」是两件事。
 */
function keySuccess(k: JEVKey): string | null {
  const calls = k.calls || 0
  if (calls <= 0) return null
  return pctText(k.ok_calls || 0, calls)
}

/**
 * 当前开关下的拦截方向，一句话。阈值字段的短提示与 ⓘ 全文共用它，
 * 因此「页面上看到的那句」与「气泡里展开的那句」永远是同一个结论。
 */
const blockRule = computed(() =>
  form.value.block_if_below ? t('settings.semanticsBelow') : t('settings.semanticsAbove'),
)
</script>

<template>
  <div class="w-full px-margin-mobile py-margin-mobile md:px-margin md:py-margin flex flex-col gap-space-lg">
    <!-- 页头 -->
    <div class="flex flex-col lg:flex-row lg:items-end justify-between gap-space-md">
      <div class="flex flex-col gap-1">
        <h1 class="text-title-2 font-title-2 text-on-surface tracking-tight">{{ t('settings.title') }}</h1>
        <p class="text-subheadline font-subheadline text-on-surface-variant">
          {{ t('settings.sub') }}
        </p>
      </div>
      <div class="flex items-center gap-space-sm">
        <span v-if="dirty" class="inline-flex items-center gap-1 text-caption-1 font-caption-1 text-tertiary">
          <span class="h-1.5 w-1.5 rounded-full bg-tertiary"></span>
          {{ t('settings.dirty') }}
        </span>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-2 rounded-xl bg-surface-container hover:bg-surface-container-high text-subheadline font-subheadline text-on-surface transition-all disabled:opacity-50"
          :disabled="saving || !dirty"
          @click="reset"
        >
          <Icon name="refresh" class="text-[16px]" />
          <span>{{ t('settings.discard') }}</span>
        </button>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-4 py-2 rounded-xl bg-primary-container hover:bg-primary-container/90 text-on-primary-container font-subheadline font-subheadline font-semibold shadow-md transition-all active:scale-[0.98] disabled:opacity-60"
          :disabled="saving"
          @click="save"
        >
          <Icon :name="saving ? 'loader' : 'check-circle'" class="text-[16px]" :class="saving ? 'animate-spin' : ''" />
          <span>{{ t('settings.save') }}</span>
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

    <!-- 语义提示：阈值方向由 block_if_below 决定。
         结论（哪一侧算拦截）留在阈值那一行的短提示里，完整推导收进 ⓘ——
         单独占一行的横幅把它放大成了页面级信息，而它其实只是这一个字段的注解。 -->

    <!-- 分组 1：上游服务与 JEV 检定路由 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-lg py-space-md border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <div class="w-9 h-9 rounded-xl bg-primary/10 flex items-center justify-center text-primary">
            <Icon name="link" class="text-[18px]" />
          </div>
          <span class="text-headline font-headline text-on-surface">{{ t('settings.group1') }}</span>
        </div>
      </div>

      <div class="px-space-lg py-space-md grid grid-cols-1 lg:grid-cols-2 gap-space-md">
        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.upstream') }}
            <HintTip :text="t('settings.upstreamTip')" />
          </span>
          <input
            v-model.trim="form.upstream_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://api.example.com"
          />
        </label>

        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.jevUrl') }}
            <HintTip :text="t('settings.jevUrlTip')" />
          </span>
          <input
            v-model.trim="form.jev_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://jev.example.com"
          />
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.jevModel') }}
            <HintTip :text="t('settings.jevModelTip')" />
          </span>
          <input
            v-model.trim="form.jev_model"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="jev-latest"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.jevModelHint') }}</span>
        </label>

        <div class="flex flex-col gap-1.5">
          <div class="flex items-center justify-between">
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.threshold') }}
              <HintTip
                :text="t('settings.semanticsTip', { v: String(form.block_if_below), rule: blockRule })"
              />
            </span>
            <span
              class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge"
              :class="thresholdValid ? 'bg-primary/15 text-primary' : 'bg-error-container text-error'"
            >
              {{ t('settings.thresholdNow', { v: thresholdDisplay }) }}
            </span>
          </div>
          <div class="flex items-center gap-space-sm">
            <input
              v-model="thresholdPct"
              type="range"
              min="0"
              max="1"
              step="0.005"
              class="flex-1 accent-primary"
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
            {{ t('settings.thresholdHint') }} · {{ blockRule }}
          </span>
        </div>

        <label class="flex flex-col gap-1.5 lg:col-span-2">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.instruction') }}
            <HintTip :text="t('settings.instructionTip')" />
          </span>
          <textarea
            v-model="form.safety_instruction"
            rows="4"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.maxChars') }}
            <HintTip :text="t('settings.maxCharsTip')" />
          </span>
          <input
            v-model.number="form.max_state_chars"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.maxCharsHint') }}</span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.timeout') }}
            <HintTip :text="t('settings.timeoutTip')" />
          </span>
          <input
            v-model.number="form.jev_timeout_ms"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.timeoutHint') }}</span>
        </label>

        <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
          <input v-model="form.expand_base64" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
          <span class="flex flex-col gap-0.5">
            <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
              {{ t('settings.base64') }}
              <HintTip :text="t('settings.base64Tip')" />
            </span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.base64Hint') }}</span>
          </span>
        </label>

        <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
          <input v-model="form.record_snippet" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
          <span class="flex flex-col gap-0.5">
            <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
              {{ t('settings.recordSnippet') }}
              <HintTip :text="t('settings.recordSnippetTip')" />
            </span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.recordSnippetHint') }}</span>
          </span>
        </label>

        <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
          <input v-model="form.dedup_enabled" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
          <span class="flex flex-col gap-0.5">
            <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
              {{ t('settings.dedup') }}
              <HintTip :text="t('settings.dedupTip')" />
            </span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.dedupHint') }}</span>
          </span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.dedupWindow') }}
            <HintTip :text="t('settings.dedupWindowTip')" />
          </span>
          <input
            v-model.number="form.dedup_window_sec"
            type="number"
            min="1"
            :disabled="!form.dedup_enabled"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.dedupWindowHint') }}</span>
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
          <span class="text-headline font-headline text-on-surface">{{ t('settings.group2') }}</span>
        </div>
        <label class="flex items-center gap-2 cursor-pointer">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ form.abuse_enabled ? t('settings.abuseOn') : t('settings.abuseOff') }}</span>
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
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.abuseWindow') }}
              <HintTip :text="t('settings.abuseWindowTip')" />
            </span>
            <input
              v-model.number="form.abuse_window_sec"
              type="number"
              min="1"
              :disabled="!form.abuse_enabled"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
            />
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.abuseWindowHint') }}</span>
          </label>

          <label class="flex flex-col gap-1.5">
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.abuseMax') }}
              <HintTip :text="t('settings.abuseMaxTip')" />
            </span>
            <input
              v-model.number="form.abuse_max_harmful"
              type="number"
              min="1"
              :disabled="!form.abuse_enabled"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset"
            />
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.abuseMaxHint') }}</span>
          </label>

          <div class="flex flex-col gap-1.5">
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.banSec') }}
              <HintTip :text="t('settings.banPresetsTip')" />
            </span>
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
                {{ t(p.labelKey) }}
              </button>
              <span class="text-caption-2 font-caption-2 text-outline">
                {{ t('settings.banNow', { v: durationOf(form.abuse_ban_sec || 0) }) }}
              </span>
            </div>
            <!-- 永久封禁：开启后，触发滥用封禁的 IP 会额外写入一条永久封禁规则
                 （内存态封禁重启即消失，规则才是真的永久）。这是让启发式判定
                 自动产生一条永久记录的开关，所以默认关闭，且只能到规则池里删规则解封。 -->
            <label class="flex items-start gap-2 cursor-pointer">
              <input
                v-model="form.abuse_ban_permanent"
                type="checkbox"
                :disabled="!form.abuse_enabled"
                class="mt-0.5 accent-primary"
              />
              <span class="flex flex-col gap-0.5">
                <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface">
                  {{ t('settings.banPermanent') }}
                  <HintTip :text="t('settings.banPermanentTip')" />
                </span>
                <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.banPermanentHint') }}</span>
              </span>
            </label>
          </div>
        </div>

        <!-- 路径门禁：只放行已知的 LLM 接口面，其余路径直接拒绝，不读 body、不送检、
             不到上游。默认关闭是刻意的：升级时 load() 把存储的 blob 叠在默认值上，
             新增字段会直接取到默认值，默认开启等于发布即静默改变现网行为。 -->
        <div class="flex flex-col gap-space-sm p-space-md rounded-xl bg-surface-container-low/70 shadow-inset">
          <label class="flex items-start gap-2 cursor-pointer">
            <input v-model="form.path_allowlist_enabled" type="checkbox" class="mt-0.5 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface">
                {{ t('settings.pathAllowlist') }}
                <HintTip :text="t('settings.pathAllowlistTip')" />
              </span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.pathAllowlistHint') }}</span>
            </span>
          </label>

          <label class="flex flex-col gap-1.5">
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.pathPrefixes') }}
              <HintTip :text="t('settings.pathPrefixesTip')" />
            </span>
            <input
              v-model="form.path_allowlist_prefixes"
              type="text"
              :disabled="!form.path_allowlist_enabled"
              placeholder="/v1/,/v1beta/"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-code-body font-code-body focus:outline-none focus:bg-surface-container-highest disabled:opacity-50 shadow-inset mono"
            />
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.pathPrefixesHint') }}</span>
          </label>

          <!-- 未知路径计 strike：只写内存态封禁，不写永久规则——猜一次路径远弱于
               一个有害载荷，永久封禁仍只留给内容。 -->
          <label class="flex items-start gap-2 cursor-pointer">
            <input
              v-model="form.abuse_count_unknown_path"
              type="checkbox"
              :disabled="!form.path_allowlist_enabled || !form.abuse_enabled"
              class="mt-0.5 accent-primary"
            />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface">
                {{ t('settings.countUnknownPath') }}
                <HintTip :text="t('settings.countUnknownPathTip')" />
              </span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.countUnknownPathHint') }}</span>
            </span>
          </label>
        </div>

        <label class="flex flex-col gap-1.5">
          <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
            {{ t('settings.blockMessage') }}
            <HintTip :text="t('settings.blockMessageTip')" />
          </span>
          <textarea
            v-model="form.block_message"
            rows="3"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-code-body font-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.blockMessageHint') }}</span>
        </label>

        <div class="flex flex-col gap-space-sm p-space-md rounded-xl bg-surface-container-low/70 shadow-inset">
          <div class="flex items-center justify-between flex-wrap gap-space-xs">
            <span class="inline-flex items-center gap-1.5 text-caption-1 font-caption-1 text-on-surface-variant">
              {{ t('settings.statusCodes') }}
              <HintTip :text="t('settings.statusCodesTip')" />
            </span>
            <div class="flex items-center gap-space-md text-caption-1 font-caption-1">
              <span class="flex items-center gap-1.5">
                <span class="px-1.5 py-0.5 rounded bg-error-container text-error font-code-badge text-code-badge mono">403</span>
                <span class="text-on-surface-variant">{{ t('settings.status403') }} · <span class="mono">X-JEV-Gateway: blocked</span></span>
              </span>
              <span class="flex items-center gap-1.5">
                <span class="px-1.5 py-0.5 rounded bg-tertiary-container/40 text-tertiary font-code-badge text-code-badge mono">429</span>
                <span class="text-on-surface-variant">{{ t('settings.status429') }} · <span class="mono">X-JEV-Gateway: banned</span></span>
              </span>
            </div>
          </div>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-space-sm">
          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.check_response" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
                {{ t('settings.checkResponse') }}
                <HintTip :text="t('settings.checkResponseTip')" />
              </span>
              <span class="text-caption-2 font-caption-2 text-tertiary">{{ t('settings.checkResponseHint') }}</span>
            </span>
          </label>

          <!-- 结论已在阈值的短提示里，这里不再重复一遍；完整说明留在 ⓘ -->
          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.block_if_below" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
                {{ t('settings.blockIfBelow') }}
                <HintTip :text="t('settings.blockIfBelowTip')" />
              </span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.fail_open" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
                {{ t('settings.failOpen') }}
                <HintTip :text="t('settings.failOpenTip')" />
              </span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.reject_oversize_body" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface">
                {{ t('settings.rejectOversize') }}
                <HintTip :text="t('settings.rejectOversizeTip')" />
              </span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.rejectOversizeHint') }}</span>
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
            <span class="text-headline font-headline text-on-surface">{{ t('settings.group3') }}</span>
          </div>
        </div>
        <div class="flex items-center gap-space-xs flex-wrap">
          <span class="inline-flex items-center px-2 py-0.5 rounded-full text-code-badge font-code-badge bg-secondary/15 text-secondary">
            {{ t('settings.roundRobin') }}
          </span>
        </div>
      </div>

      <div class="px-space-lg py-space-md flex flex-col gap-space-md">
        <div class="flex items-end gap-space-sm flex-wrap">
          <label class="flex flex-col gap-1.5 flex-1 min-w-[10rem]">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.keyLabel') }}</span>
            <input
              v-model="newLabel"
              type="text"
              :placeholder="t('settings.keyLabelPlaceholder')"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-subheadline font-subheadline focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            />
          </label>
          <label class="flex flex-col gap-1.5 flex-[2] min-w-[14rem]">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.keyBody') }}</span>
            <input
              v-model="newKey"
              type="password"
              autocomplete="off"
              :placeholder="t('settings.keyBodyPlaceholder')"
              class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
              @keydown.enter.prevent="addKey"
            />
          </label>
          <button
            type="button"
            class="inline-flex items-center gap-1.5 px-4 py-2.5 rounded-xl bg-primary-container hover:bg-primary-container/90 text-on-primary-container text-subheadline font-subheadline font-semibold shadow-md transition-all active:scale-[0.98] disabled:opacity-60"
            :disabled="keyBusy"
            @click="addKey"
          >
            <Icon :name="keyBusy ? 'loader' : 'key'" class="text-[16px]" :class="keyBusy ? 'animate-spin' : ''" />
            <span>{{ t('settings.addKey') }}</span>
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
                <span class="text-caption-1 font-caption-1 text-on-surface mono">{{ t('settings.keyCalls', { n: num(k.calls) }) }}</span>
                <span class="text-caption-2 font-caption-2 text-outline">
                  {{ t('settings.keyLastUsed', { v: relativeOf(k.last_used) }) }}
                </span>
              </div>
              <!-- 成功率与最近一次失败原因：分母是尝试次数，所以「成功 0 / 尝试 0」
                   显示为「—」（从未用过）而不是 0%。 -->
              <div class="flex flex-col" :title="k.last_error || ''">
                <span class="text-caption-1 font-caption-1 mono" :class="k.err_calls ? 'text-error' : 'text-on-surface'">
                  {{ keySuccess(k) ?? '—' }}
                </span>
                <span class="text-caption-2 font-caption-2 text-outline">
                  {{ t('settings.keyOkErr', { ok: num(k.ok_calls), err: num(k.err_calls) }) }}
                </span>
              </div>
              <label
                class="relative inline-flex items-center cursor-pointer"
                :title="k.enabled ? t('settings.keyDisable') : t('settings.keyEnable')"
              >
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
                <span>{{ t('settings.delete') }}</span>
              </button>
            </div>
          </div>

          <div v-if="!keyPool.length" class="px-space-md py-space-lg flex flex-col items-center gap-1.5">
            <Icon name="key" class="text-outline text-[22px]" />
            <span class="text-subheadline font-subheadline text-on-surface-variant">{{ t('settings.keysEmpty') }}</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.keysEmptyHint') }}</span>
          </div>
        </div>

        <!-- 四格全部是真实计数：轮询分发没有权重，也就没有「池负载」可算，
             所以这里只放能由 ok_calls / err_calls 直接得出的事实。 -->
        <div class="grid grid-cols-2 md:grid-cols-4 gap-space-sm">
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">{{ t('settings.keysEnabledTitle') }}</span>
            <span class="text-headline font-headline text-on-surface mono">{{ keysEnabled }} / {{ keyPool.length }}</span>
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">{{ t('settings.callsTotal') }}</span>
            <span class="text-headline font-headline text-on-surface mono">{{ num(totalCalls) }}</span>
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">{{ t('settings.successRate') }}</span>
            <span class="text-headline font-headline mono" :class="errCalls ? 'text-error' : 'text-on-surface'">
              {{ successRate ?? '—' }}
            </span>
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">{{ t('settings.callsFailed') }}</span>
            <span class="text-headline font-headline text-on-surface mono">{{ num(errCalls) }}</span>
          </div>
        </div>

        <div class="flex items-start gap-1.5 p-space-md rounded-xl bg-surface-container-low/70 shadow-inset">
          <span class="inline-flex items-center gap-1.5 text-subheadline font-subheadline text-on-surface-variant">
            {{ t('settings.compliance') }}
            <HintTip :text="t('settings.complianceFoot')" />
          </span>
        </div>
      </div>
    </section>
  </div>
</template>
