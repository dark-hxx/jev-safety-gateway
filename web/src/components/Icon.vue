<script setup lang="ts">
/**
 * 本地内联 SVG 图标集。
 *
 * 原型使用 Material Symbols 字体图标（Google Fonts CDN）。离线约束下不能引外链字体，
 * 也不能内嵌字体文件，因此这里以手绘的 24×24 描边图标等价替代：语义、尺寸与
 * 视觉重量对齐原型，且不产生任何网络请求。
 *
 * 用法：尺寸由 font-size 控制（1em），例如 `<Icon name="key" class="text-[18px]" />`。
 */
const ICONS: Record<string, string> = {
  'verified-user': `
    <path d="M12 3l7 3v5.5c0 4.2-2.9 7.9-7 9.5-4.1-1.6-7-5.3-7-9.5V6z"/>
    <path d="M9 12.2l2 2 4-4.2"/>`,
  eye: `
    <path d="M2.5 12S6 5.8 12 5.8 21.5 12 21.5 12 18 18.2 12 18.2 2.5 12 2.5 12z"/>
    <circle cx="12" cy="12" r="2.8"/>`,
  'eye-off': `
    <path d="M4 4l16 16"/>
    <path d="M9.6 5.9A9.6 9.6 0 0 1 12 5.6c6 0 9.5 6.2 9.5 6.2a17 17 0 0 1-3 3.9"/>
    <path d="M6.3 7.9A17 17 0 0 0 2.5 12s3.5 6.2 9.5 6.2a9.4 9.4 0 0 0 3.4-.6"/>
    <path d="M9.7 10a3 3 0 0 0 4.2 4.2"/>`,
  'arrow-right': `
    <path d="M4 12h15"/>
    <path d="M13 6l6 6-6 6"/>`,
  login: `
    <path d="M13.5 4H19a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-5.5"/>
    <path d="M4 12h10"/>
    <path d="M11 8.5l3.5 3.5-3.5 3.5"/>`,
  pin: `
    <path d="M12 3.5c2.5 0 4.2 1.9 4.2 4.3 0 2.1-.9 3.5-1.8 4.6-.6.8-.9 1.3-.9 2.2v1.1h-3v-1.1c0-.9-.3-1.4-.9-2.2-.9-1.1-1.8-2.5-1.8-4.6 0-2.4 1.7-4.3 4.2-4.3z"/>
    <path d="M12 15.7v4.8"/>`,
  memory: `
    <rect x="7" y="7" width="10" height="10" rx="2"/>
    <path d="M10 3.5v3M14 3.5v3M10 17.5v3M14 17.5v3M3.5 10h3M3.5 14h3M17.5 10h3M17.5 14h3"/>`,
  encrypted: `
    <rect x="4.5" y="10" width="15" height="9.5" rx="2"/>
    <path d="M8.2 10V7.6a3.8 3.8 0 0 1 7.6 0V10"/>
    <path d="M12 14v2.2"/>`,
  rule: `
    <path d="M4.5 5.5h15v13h-15z"/>
    <path d="M9.5 5.5v13"/>
    <path d="M12.5 9.5h4M12.5 13h4"/>`,
  'shield-lock': `
    <path d="M12 3l7 3v5.5c0 4.2-2.9 7.9-7 9.5-4.1-1.6-7-5.3-7-9.5V6z"/>
    <rect x="9.8" y="10.6" width="4.4" height="3.6" rx="0.9"/>
    <path d="M10.8 10.6V9.7a1.2 1.2 0 0 1 2.4 0v0.9"/>`,
  shield: '<path d="M12 3l7 3v5.5c0 4.2-2.9 7.9-7 9.5-4.1-1.6-7-5.3-7-9.5V6z"/>',
  'shield-check': `
    <path d="M12 3l7 3v5.5c0 4.2-2.9 7.9-7 9.5-4.1-1.6-7-5.3-7-9.5V6z"/>
    <path d="M9 12.2l2 2 4-4.2"/>`,
  'shield-user': `
    <path d="M12 3l7 3v5.5c0 4.2-2.9 7.9-7 9.5-4.1-1.6-7-5.3-7-9.5V6z"/>
    <circle cx="12" cy="10.6" r="1.9"/>
    <path d="M8.8 15.6a3.6 3.6 0 0 1 6.4 0"/>`,
  logout: `
    <path d="M14.5 4H19a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4.5"/>
    <path d="M10 8l-4 4 4 4"/>
    <path d="M6 12h9"/>`,
  person: `
    <circle cx="12" cy="8" r="3.4"/>
    <path d="M5.6 19.6a6.4 6.4 0 0 1 12.8 0"/>`,
  server: `
    <rect x="3" y="4" width="18" height="7" rx="2"/>
    <rect x="3" y="13" width="18" height="7" rx="2"/>
    <path d="M7 7.5h.01M7 16.5h.01"/>`,
  key: `
    <circle cx="8" cy="15.5" r="3.6"/>
    <path d="M10.6 12.9L20 3.5"/>
    <path d="M15.6 4.4l3 3"/>
    <path d="M17.9 7.9l2.1-2.1"/>`,
  lock: `
    <rect x="4.5" y="10.5" width="15" height="9.5" rx="2.2"/>
    <path d="M8.2 10.5V8a3.8 3.8 0 0 1 7.6 0v2.5"/>`,
  sliders: `
    <path d="M4 7h16M4 12h16M4 17h16"/>
    <path d="M9.5 5.2v3.6M16 10.2v3.6M7.5 15.2v3.6"/>`,
  search: `
    <circle cx="10.6" cy="10.6" r="6.4"/>
    <path d="M15.4 15.4L20.5 20.5"/>`,
  'search-check': `
    <circle cx="10.6" cy="10.6" r="6.4"/>
    <path d="M8.2 10.7l1.7 1.7 3-3.2"/>
    <path d="M15.4 15.4L20.5 20.5"/>`,
  hub: `
    <circle cx="12" cy="12" r="2.6"/>
    <circle cx="12" cy="4" r="1.9"/>
    <circle cx="4.4" cy="18" r="1.9"/>
    <circle cx="19.6" cy="18" r="1.9"/>
    <path d="M12 6.4V9.4M10 13.6l-4 3.1M14 13.6l4 3.1"/>`,
  link: `
    <path d="M10.4 13a4.7 4.7 0 0 0 7 .6l2.4-2.4a4.7 4.7 0 0 0-6.6-6.6L11.7 6"/>
    <path d="M13.6 11a4.7 4.7 0 0 0-7-.6l-2.4 2.4a4.7 4.7 0 0 0 6.6 6.6L12.3 18"/>`,
  activity: '<path d="M21 12h-3.6l-3 7.5-4.8-15L7 12H3"/>',
  'chevron-down': '<path d="M6.5 9.5l5.5 5.5 5.5-5.5"/>',
  'chevron-left': '<path d="M14.5 6.5L9 12l5.5 5.5"/>',
  'chevron-right': '<path d="M9.5 6.5L15 12l-5.5 5.5"/>',
  'chevrons-left': '<path d="M12.5 6.5L7 12l5.5 5.5"/><path d="M18 6.5L12.5 12l5.5 5.5"/>',
  'chevrons-right': '<path d="M11.5 6.5L17 12l-5.5 5.5"/><path d="M6 6.5L11.5 12L6 17.5"/>',
  'check-circle': `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M8.2 12.4l2.6 2.6 4.9-5.3"/>`,
  gavel: `
    <path d="M13.6 3.8l6.6 6.6"/>
    <path d="M11.4 6l6.6 6.6"/>
    <path d="M3 20.6h8.4"/>
    <path d="M7.6 16.4l5.2-5.2"/>`,
  ban: `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M6.1 6.1l11.8 11.8"/>`,
  refresh: `
    <path d="M20.5 12a8.5 8.5 0 0 1-14.6 5.9L3.5 15.5"/>
    <path d="M3.5 12A8.5 8.5 0 0 1 18.1 6.1L20.5 8.5"/>
    <path d="M20.5 4.4v4.1h-4.1"/>
    <path d="M3.5 19.6v-4.1h4.1"/>`,
  hand: `
    <path d="M8.4 13.4V6.2a1.7 1.7 0 0 1 3.4 0v5.6"/>
    <path d="M11.8 11.6V5.2a1.7 1.7 0 0 1 3.4 0v6.6"/>
    <path d="M15.2 11.9V7.6a1.7 1.7 0 0 1 3.4 0v7.2a6 6 0 0 1-6 6h-1a5 5 0 0 1-3.9-1.9L4.4 15.4a1.75 1.75 0 0 1 2.6-2.3l1.4 1.5"/>`,
  'alert-triangle': `
    <path d="M12 4.2l8.4 14.9H3.6z"/>
    <path d="M12 10v4"/>
    <path d="M12 17.2h.01"/>`,
  'alert-circle': `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M12 7.8v4.8M12 16.2h.01"/>`,
  info: `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M12 11.2v5M12 7.9h.01"/>`,
  download: `
    <path d="M12 3.6v11.4"/>
    <path d="M7.4 10.4L12 15l4.6-4.6"/>
    <path d="M4.4 20.4h15.2"/>`,
  plus: '<path d="M12 5v14M5 12h14"/>',
  trash: `
    <path d="M4 7h16"/>
    <path d="M9 7V5.4A1.4 1.4 0 0 1 10.4 4h3.2A1.4 1.4 0 0 1 15 5.4V7"/>
    <path d="M6.5 7l.9 12.1A1.6 1.6 0 0 0 9 20.6h6a1.6 1.6 0 0 0 1.6-1.5L17.5 7"/>
    <path d="M10 11v6M14 11v6"/>`,
  'filter-off': `
    <path d="M4.6 5.4h14.8l-5.9 6.9v6.3l-3-1.8v-4.5z"/>
    <path d="M3.4 3.4l17.2 17.2"/>`,
  network: `
    <rect x="9" y="3" width="6" height="5" rx="1.4"/>
    <rect x="2.4" y="16" width="6" height="5" rx="1.4"/>
    <rect x="15.6" y="16" width="6" height="5" rx="1.4"/>
    <path d="M12 8v4M5.4 16v-2.4h13.2V16"/>`,
  bot: `
    <rect x="4.6" y="8.2" width="14.8" height="11" rx="3.2"/>
    <path d="M12 8.2V5.2"/>
    <path d="M9.6 19.2v1.6M14.4 19.2v1.6"/>
    <path d="M9.6 12.6h.01M14.4 12.6h.01"/>`,
  'caret-down': '<path d="M7.4 10.2l4.6 4.6 4.6-4.6z"/>',
  menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  x: '<path d="M6.2 6.2l11.6 11.6M17.8 6.2L6.2 17.8"/>',
  zap: '<path d="M13.2 2.6L4.8 13.6h6.1l-1 7.8 8.4-11h-6.1z"/>',
  loader: `
    <path d="M12 3v3.6M12 17.4V21M3 12h3.6M17.4 12H21"/>
    <path d="M5.6 5.6l2.5 2.5M15.9 15.9l2.5 2.5M18.4 5.6l-2.5 2.5M8.1 15.9l-2.5 2.5"/>`,
  clock: `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M12 7.2V12l3.2 2"/>`,
  'trending-up': `
    <path d="M3.4 17.4l5.7-5.7 3.9 3.9 7.6-7.6"/>
    <path d="M15.6 8h5v5"/>`,
  'trending-down': `
    <path d="M3.4 6.6l5.7 5.7 3.9-3.9 7.6 7.6"/>
    <path d="M15.6 16h5v-5"/>`,
  'arrow-up-right': `
    <path d="M7 17L17 7"/>
    <path d="M8.4 7H17v8.6"/>`,
  'swap-horiz': `
    <path d="M7.6 4.4L4 8l3.6 3.6"/>
    <path d="M4 8h16"/>
    <path d="M16.4 19.6L20 16l-3.6-3.6"/>
    <path d="M20 16H4"/>`,
  'alt-route': `
    <path d="M4 20.2h5.2a4 4 0 0 0 4-4v-4a4 4 0 0 1 4-4h2.6"/>
    <path d="M17 5.4l3 3-3 3"/>`,
  gauge: `
    <path d="M4 17.6a9 9 0 1 1 16 0"/>
    <path d="M12 13.6l4.2-3.4"/>`,
  donut: `
    <circle cx="12" cy="12" r="8.4"/>
    <circle cx="12" cy="12" r="3"/>`,
  sparkles: `
    <path d="M12 3.6l1.6 4.2 4.2 1.6-4.2 1.6L12 15.2l-1.6-4.2L6.2 9.4l4.2-1.6z"/>
    <path d="M18 15.4l.8 2 2 .8-2 .8-.8 2-.8-2-2-.8 2-.8z"/>`,
  layers: `
    <path d="M12 3.6l8.4 4.4-8.4 4.4-8.4-4.4z"/>
    <path d="M3.6 12.4L12 16.8l8.4-4.4"/>
    <path d="M3.6 16.6L12 21l8.4-4.4"/>`,
  // 主题切换三态（跟随系统 / 浅色 / 深色）
  monitor: `
    <rect x="3" y="4.4" width="18" height="12.6" rx="2"/>
    <path d="M9 20.4h6M12 17v3.4"/>`,
  sun: `
    <circle cx="12" cy="12" r="4.2"/>
    <path d="M12 2.6v2.2M12 19.2v2.2M2.6 12h2.2M19.2 12h2.2"/>
    <path d="M5.4 5.4l1.6 1.6M17 17l1.6 1.6M18.6 5.4L17 7M7 17l-1.6 1.6"/>`,
  moon: `
    <path d="M20 14.4A8.4 8.4 0 0 1 9.6 4a8.6 8.6 0 1 0 10.4 10.4z"/>`,
  globe: `
    <circle cx="12" cy="12" r="8.8"/>
    <path d="M3.4 12h17.2"/>
    <path d="M12 3.2c2.2 2.3 3.4 5.4 3.4 8.8s-1.2 6.5-3.4 8.8c-2.2-2.3-3.4-5.4-3.4-8.8s1.2-6.5 3.4-8.8z"/>`,
}

defineProps<{ name: string }>()
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    width="1em"
    height="1em"
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    v-html="ICONS[name] ?? ICONS['info']"
  />
</template>
