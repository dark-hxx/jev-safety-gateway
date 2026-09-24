/**
 * Tailwind 主题：以 docs/prototype/cupertino_safety_gateway/DESIGN.md 为基线。
 *
 * 与原型（docs/prototype/<screen>/code.html）的差异仅限于「离线资源替换」：
 * - 字体族：原型引用 Google Fonts（Plus Jakarta Sans / JetBrains Mono），
 *   这里替换为等价的**系统字体栈**——不引 CDN、不内嵌字体文件、不新增网络请求。
 * - 图标：原型引用 Material Symbols 字体图标，这里替换为本地内联 SVG（components/Icon.vue）。
 * 调色板、字阶、8pt 间距体系、发丝边框与材质层级均按 DESIGN.md 落地。
 * 调色板改为跟随主题：语义色在这里只是指向 src/style.css 中 CSS 变量的引用（见下方 `token()`），
 * 深浅两套色值集中定义在那里，组件侧只写语义名。
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

/**
 * 语义色令牌 → CSS 变量引用。
 *
 * 变量在 `src/style.css` 里按 `.light` / `.dark` 两组定义，值是空格分隔的 R G B 三元组；
 * 这里保留 `<alpha-value>`，让 `bg-secondary/15`、`border-error/40` 一类透明度修饰符
 * 仍然按 Tailwind 的写法生效。`<html>` 上的主题类由 index.html 的首屏脚本与 `src/theme.ts` 维护。
 */
const token = (name) => `rgb(var(--c-${name}) / <alpha-value>)`

export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{vue,ts}'],
  theme: {
    extend: {
      colors: {
        // —— DESIGN.md 调色板的语义色 ——
        // 每条都是指向 style.css 里 CSS 变量的三元组引用：深浅两套色值只在那里定义一次，
        // 组件里只写语义名（bg-surface / text-secondary），不写 dark: 变体。
        // <alpha-value> 让 bg-secondary/15 一类透明度修饰符继续可用。
        surface: token('surface'),
        'surface-dim': token('surface-dim'),
        'surface-bright': token('surface-bright'),
        'surface-container-lowest': token('surface-container-lowest'),
        'surface-container-low': token('surface-container-low'),
        'surface-container': token('surface-container'),
        'surface-container-high': token('surface-container-high'),
        'surface-container-highest': token('surface-container-highest'),
        // 灰底之上的「提亮块」（侧栏选中项 / 侧栏徽标底 / 顶栏状态丸）：M3 色板里没有
        // 对应角色，深浅两档取值见 style.css —— 浅色下是白，深色下等于 surface-container-high。
        'surface-raised': token('surface-raised'),
        'on-surface': token('on-surface'),
        'on-surface-variant': token('on-surface-variant'),
        'inverse-surface': token('inverse-surface'),
        'inverse-on-surface': token('inverse-on-surface'),
        outline: token('outline'),
        'outline-variant': token('outline-variant'),
        'surface-variant': token('surface-variant'),
        'surface-tint': token('surface-tint'),
        primary: token('primary'),
        'on-primary': token('on-primary'),
        'primary-container': token('primary-container'),
        'on-primary-container': token('on-primary-container'),
        'primary-fixed': token('primary-fixed'),
        'primary-fixed-dim': token('primary-fixed-dim'),
        'on-primary-fixed': token('on-primary-fixed'),
        'on-primary-fixed-variant': token('on-primary-fixed-variant'),
        'inverse-primary': token('inverse-primary'),
        secondary: token('secondary'),
        'on-secondary': token('on-secondary'),
        'secondary-container': token('secondary-container'),
        'on-secondary-container': token('on-secondary-container'),
        'secondary-fixed': token('secondary-fixed'),
        'secondary-fixed-dim': token('secondary-fixed-dim'),
        'on-secondary-fixed': token('on-secondary-fixed'),
        'on-secondary-fixed-variant': token('on-secondary-fixed-variant'),
        tertiary: token('tertiary'),
        'on-tertiary': token('on-tertiary'),
        'tertiary-container': token('tertiary-container'),
        'on-tertiary-container': token('on-tertiary-container'),
        'tertiary-fixed': token('tertiary-fixed'),
        'tertiary-fixed-dim': token('tertiary-fixed-dim'),
        'on-tertiary-fixed': token('on-tertiary-fixed'),
        'on-tertiary-fixed-variant': token('on-tertiary-fixed-variant'),
        error: token('error'),
        'on-error': token('on-error'),
        'error-container': token('error-container'),
        'on-error-container': token('on-error-container'),
        background: token('background'),
        'on-background': token('on-background'),
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
        // 发丝边框（DESIGN.md separators & fills）：深浅两套取值见 style.css 的 --hairline。
        hairline: 'var(--hairline)',
      },
      boxShadow: {
        // DESIGN.md 材质层级：Elevation 2 的扩散阴影。阴影在浅色下要明显变淡，
        // 因此与调色板一样按主题取值（见 style.css）。
        overlay: 'var(--shadow-overlay)',
        inset: 'var(--shadow-inset)',
      },
    },
  },
  plugins: [],
}
