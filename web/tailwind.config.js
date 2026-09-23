/**
 * Tailwind 主题：以 docs/prototype/cupertino_safety_gateway/DESIGN.md 为基线。
 *
 * 与原型（docs/prototype/<screen>/code.html）的差异仅限于「离线资源替换」：
 * - 字体族：原型引用 Google Fonts（Plus Jakarta Sans / JetBrains Mono），
 *   这里替换为等价的**系统字体栈**——不引 CDN、不内嵌字体文件、不新增网络请求。
 * - 图标：原型引用 Material Symbols 字体图标，这里替换为本地内联 SVG（components/Icon.vue）。
 * 调色板、字阶、8pt 间距体系、发丝边框与材质层级均按 DESIGN.md 落地。
 */

/** 正文字体栈：优先系统上可用的优质无衬线字体，保留通用兜底。 */
const SANS_STACK = [
  'Inter',
  '-apple-system',
  'BlinkMacSystemFont',
  '"Segoe UI"',
  '"Source Han Sans SC"',
  '"Noto Sans CJK SC"',
  '"PingFang SC"',
  '"Hiragino Sans GB"',
  '"Microsoft YaHei"',
  'sans-serif',
]

/** 等宽字体栈：时间戳、路径、密钥、IP、指标等代码型内容。 */
const MONO_STACK = [
  '"JetBrains Mono"',
  '"SF Mono"',
  '"Cascadia Code"',
  'Consolas',
  '"Liberation Mono"',
  'ui-monospace',
  'monospace',
]

/** DESIGN.md 的字阶表：字号 + 行高 + 字重 + 字距。 */
const typeScale = {
  display: ['34px', { lineHeight: '41px', letterSpacing: '-0.02em', fontWeight: '700' }],
  'display-mobile': ['28px', { lineHeight: '34px', letterSpacing: '-0.015em', fontWeight: '700' }],
  'title-1': ['28px', { lineHeight: '34px', letterSpacing: '-0.015em', fontWeight: '700' }],
  'title-2': ['22px', { lineHeight: '28px', letterSpacing: '-0.01em', fontWeight: '600' }],
  'title-3': ['20px', { lineHeight: '25px', letterSpacing: '-0.008em', fontWeight: '600' }],
  headline: ['17px', { lineHeight: '22px', letterSpacing: '-0.005em', fontWeight: '600' }],
  body: ['16px', { lineHeight: '21px', letterSpacing: '-0.003em', fontWeight: '400' }],
  callout: ['15px', { lineHeight: '20px', letterSpacing: '-0.002em', fontWeight: '400' }],
  subheadline: ['14px', { lineHeight: '18px', fontWeight: '500' }],
  footnote: ['13px', { lineHeight: '18px', fontWeight: '400' }],
  'caption-1': ['12px', { lineHeight: '16px', fontWeight: '500' }],
  'caption-2': ['11px', { lineHeight: '13px', fontWeight: '600' }],
  'code-body': ['13px', { lineHeight: '18px', fontWeight: '400' }],
  'code-badge': ['11px', { lineHeight: '14px', letterSpacing: '0.02em', fontWeight: '500' }],
}

/** 字阶令牌 → 系统字体栈映射（原型中 SANS 与 MONO 两类）。 */
const fontFamilies = {
  sans: SANS_STACK,
  mono: MONO_STACK,
}
for (const name of Object.keys(typeScale)) {
  fontFamilies[name] = name.startsWith('code-') ? MONO_STACK : SANS_STACK
}

export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        // —— DESIGN.md 调色板（暗色为默认模式）——
        surface: '#121317',
        'surface-dim': '#121317',
        'surface-bright': '#38393d',
        'surface-container-lowest': '#0d0e12',
        'surface-container-low': '#1a1b1f',
        'surface-container': '#1e1f23',
        'surface-container-high': '#292a2e',
        'surface-container-highest': '#343539',
        'on-surface': '#e3e2e7',
        'on-surface-variant': '#c1c6d7',
        'inverse-surface': '#e3e2e7',
        'inverse-on-surface': '#2f3034',
        outline: '#8b90a0',
        'outline-variant': '#414755',
        'surface-variant': '#343539',
        'surface-tint': '#adc6ff',
        primary: '#adc6ff',
        'on-primary': '#002e69',
        'primary-container': '#4b8eff',
        'on-primary-container': '#00285c',
        'primary-fixed': '#d8e2ff',
        'primary-fixed-dim': '#adc6ff',
        'on-primary-fixed': '#001a41',
        'on-primary-fixed-variant': '#004493',
        'inverse-primary': '#005bc1',
        secondary: '#53e16f',
        'on-secondary': '#003911',
        'secondary-container': '#05b046',
        'on-secondary-container': '#003a11',
        'secondary-fixed': '#72fe88',
        'secondary-fixed-dim': '#53e16f',
        'on-secondary-fixed': '#002107',
        'on-secondary-fixed-variant': '#00531c',
        tertiary: '#ffb874',
        'on-tertiary': '#4b2800',
        'tertiary-container': '#d47b00',
        'on-tertiary-container': '#412200',
        'tertiary-fixed': '#ffdcbf',
        'tertiary-fixed-dim': '#ffb874',
        'on-tertiary-fixed': '#2d1600',
        'on-tertiary-fixed-variant': '#6a3b00',
        error: '#ffb4ab',
        'on-error': '#690005',
        'error-container': '#93000a',
        'on-error-container': '#ffdad6',
        background: '#121317',
        'on-background': '#e3e2e7',
      },
      fontFamily: fontFamilies,
      fontSize: typeScale,
      spacing: {
        // 8pt 空间体系（DESIGN.md spacing 令牌）
        gutter: '1.25rem',
        'gutter-mobile': '0.75rem',
        margin: '2rem',
        'margin-mobile': '1rem',
        'space-xs': '0.25rem',
        'space-sm': '0.5rem',
        'space-md': '1rem',
        'space-lg': '1.5rem',
        'space-xl': '2.25rem',
      },
      borderColor: {
        // 发丝边框（DESIGN.md separators & fills）
        hairline: 'rgba(255, 255, 255, 0.12)',
      },
      boxShadow: {
        // DESIGN.md 材质层级：Elevation 2 的扩散阴影
        overlay: '0 12px 32px -4px rgba(0, 0, 0, 0.48)',
        inset: 'inset 0 1px 0 rgba(255, 255, 255, 0.04)',
      },
    },
  },
  plugins: [],
}
