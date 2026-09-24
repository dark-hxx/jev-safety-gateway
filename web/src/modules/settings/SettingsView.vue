<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import Icon from '../../components/Icon.vue'
import NotConnected from '../../components/NotConnected.vue'
import * as api from '../../api'
import { useConsole } from '../../console'
import { durationOf, num, relativeOf } from '../../format'
import { useI18n } from '../../i18n'
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

/** 封禁时长预设。后端 `abuse_ban_sec` 是秒数，因此不提供「永久」预设。 */
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
  if (!window.confirm(t('settings.deleteKeyConfirm', { label: k.label }))) return
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

    <!-- 语义提示：阈值方向由 block_if_below 决定 -->
    <div class="flex items-start gap-2 px-space-md py-2.5 rounded-xl bg-surface-container border border-hairline">
      <Icon name="info" class="text-primary text-[16px] mt-0.5 shrink-0" />
      <span class="text-caption-1 font-caption-1 text-on-surface-variant leading-relaxed">
        <!-- 插值行内代码片必须与相邻插值同处一行：换行会被 Vue 的空白压缩吃掉。
             片段之间的空格属于**该语言的排版**（中文「，因此」后面不要空格，英文「, so 」后面要），
             因此把它写在文案表里，而不是让模板替两种语言做同一个决定。 -->
        {{ t('settings.semanticsPre') }} <span class="font-code-badge text-code-badge text-primary mono">block_if_below = {{ form.block_if_below }}</span>{{ t('settings.semanticsMid') }}<span class="text-on-surface">{{ form.block_if_below ? t('settings.semanticsBelow') : t('settings.semanticsAbove') }}</span>{{ t('settings.semanticsTail') }}
      </span>
    </div>

    <!-- 分组 1：上游服务与 JEV 检定路由 -->
    <section class="flex flex-col rounded-2xl bg-surface-container shadow-lg border border-hairline overflow-hidden">
      <div class="px-space-lg py-space-md border-b border-hairline flex items-center justify-between flex-wrap gap-space-xs">
        <div class="flex items-center gap-space-sm">
          <div class="w-9 h-9 rounded-xl bg-primary/10 flex items-center justify-center text-primary">
            <Icon name="link" class="text-[18px]" />
          </div>
          <span class="text-headline font-headline text-on-surface">{{ t('settings.group1') }}</span>
        </div>
        <NotConnected :reason="t('settings.noProbe')" />
      </div>

      <div class="px-space-lg py-space-md grid grid-cols-1 lg:grid-cols-2 gap-space-md">
        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.upstream') }}</span>
          <input
            v-model.trim="form.upstream_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://api.example.com"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.upstreamHint') }}</span>
        </label>

        <label class="flex flex-col gap-1.5 lg:col-span-1">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.jevUrl') }}</span>
          <input
            v-model.trim="form.jev_base_url"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="https://jev.example.com"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.jevUrlHint') }} <span class="mono">/v1/systemone</span>{{ t('settings.jevUrlHintTail') }}</span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.jevModel') }}</span>
          <input
            v-model.trim="form.jev_model"
            type="text"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
            placeholder="jev-latest"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.jevModelHint') }} <span class="mono">jev-latest</span>{{ t('settings.jevModelHintTail') }}</span>
        </label>

        <div class="flex flex-col gap-1.5">
          <div class="flex items-center justify-between">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.threshold') }}</span>
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
            {{ t('settings.thresholdHint') }}
          </span>
        </div>

        <label class="flex flex-col gap-1.5 lg:col-span-2">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.instruction') }}</span>
          <textarea
            v-model="form.safety_instruction"
            rows="4"
            spellcheck="false"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
          <span class="text-caption-2 font-caption-2 text-outline">
            {{ t('settings.instructionHint') }}
          </span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.maxChars') }}</span>
          <input
            v-model.number="form.max_state_chars"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.maxCharsHint') }}</span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.timeout') }}</span>
          <input
            v-model.number="form.jev_timeout_ms"
            type="number"
            min="1"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface font-code-body text-code-body focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset"
          />
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.timeoutHint') }}</span>
        </label>

        <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
          <input v-model="form.record_snippet" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
          <span class="flex flex-col gap-0.5">
            <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.recordSnippet') }}</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.recordSnippetHintPre') }}<span class="mono">JEV_DEBUG</span>{{ t('settings.recordSnippetHintPost') }}</span>
          </span>
        </label>

        <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
          <input v-model="form.dedup_enabled" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
          <span class="flex flex-col gap-0.5">
            <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.dedup') }}</span>
            <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.dedupHint') }}</span>
          </span>
        </label>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.dedupWindow') }}</span>
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
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.abuseWindow') }}</span>
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
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.abuseMax') }}</span>
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
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.banSec') }}</span>
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
            <span class="text-caption-2 font-caption-2 text-outline">
              <NotConnected :reason="t('settings.banNoPermanent')" /> {{ t('settings.banPresetsHint') }}
            </span>
          </div>
        </div>

        <label class="flex flex-col gap-1.5">
          <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.blockMessage') }}</span>
          <textarea
            v-model="form.block_message"
            rows="3"
            class="w-full px-3 py-2.5 rounded-xl bg-surface-container-high text-on-surface text-code-body font-code-body leading-relaxed focus:outline-none focus:bg-surface-container-highest transition-colors shadow-inset resize-y"
          ></textarea>
          <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.blockMessageHint') }}</span>
        </label>

        <div class="flex flex-col gap-space-sm p-space-md rounded-xl bg-surface-container-low/70 shadow-inset">
          <div class="flex items-center justify-between flex-wrap gap-space-xs">
            <span class="text-caption-1 font-caption-1 text-on-surface-variant">{{ t('settings.statusCodes') }}</span>
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
          <span class="text-caption-2 font-caption-2 text-outline">
            {{ t('settings.statusCodesHint') }}
          </span>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-space-sm">
          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.check_response" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.checkResponse') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.checkResponseHintPre') }}<span class="text-tertiary">{{ t('settings.checkResponseHintWarn') }}</span>{{ t('settings.checkResponseHintPost') }}</span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.block_if_below" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.blockIfBelow') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.blockIfBelowHint') }}</span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.fail_open" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.failOpen') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.failOpenHint') }}</span>
            </span>
          </label>

          <label class="flex items-start gap-space-sm p-space-sm rounded-xl bg-surface-container-low/70 cursor-pointer">
            <input v-model="form.reject_oversize_body" type="checkbox" class="mt-0.5 h-4 w-4 accent-primary" />
            <span class="flex flex-col gap-0.5">
              <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.rejectOversize') }}</span>
              <span class="text-caption-2 font-caption-2 text-outline">{{ t('settings.rejectOversizeHintPre') }}<span class="mono">413</span>{{ t('settings.rejectOversizeHintPost') }}</span>
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
          <NotConnected :reason="t('settings.noWeight')" />
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
              <NotConnected :reason="t('settings.keySuccessReason')" />
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
            <span class="eyebrow">{{ t('settings.heartbeat') }}</span>
            <NotConnected :reason="t('settings.heartbeatReason')" />
          </div>
          <div class="flex flex-col p-space-sm rounded-xl bg-surface-container-low/70">
            <span class="eyebrow">{{ t('settings.poolLoad') }}</span>
            <NotConnected :reason="t('settings.poolLoadReason')" />
          </div>
        </div>

        <div class="flex flex-col gap-space-xs p-space-md rounded-xl border border-dashed border-outline-variant/60">
          <div class="flex items-center justify-between flex-wrap gap-space-xs">
            <span class="text-subheadline font-subheadline text-on-surface">{{ t('settings.compliance') }}</span>
            <NotConnected :reason="t('settings.complianceReason')" />
          </div>
          <span class="text-caption-2 font-caption-2 text-outline">
            {{ t('settings.complianceFoot') }}
          </span>
        </div>
      </div>
    </section>
  </div>
</template>
