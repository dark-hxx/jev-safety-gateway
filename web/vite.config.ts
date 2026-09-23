import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 构建产物提交入库，并由 web/embed.go 通过 //go:embed 打包进 Go 二进制。
// 因此这里不做 hash 命名之外的特殊处理：产物目录固定为 web/dist。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // 控制台只在内网管理口（默认 127.0.0.1）加载，无需为公网 CDN 做额外拆包。
    assetsInlineLimit: 4096,
  },
})
