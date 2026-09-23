package web

import (
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"
)

// 这些测试锁定「离线可用 + 构建产物齐备」两条验收不变量。它们不依赖 Node：
// dist 与源码一样提交入库，因此可以在没有前端工具链的环境里直接校验提交内容。

func TestEmbedHasIndexHTML(t *testing.T) {
	body, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("embedded index.html missing: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("embedded index.html is empty")
	}
	if !strings.Contains(string(body), `id="app"`) {
		t.Fatal(`index.html does not contain the Vue mount point id="app"`)
	}
}

// index.html 引用的 /assets/* 必须在嵌入文件里真实存在，防止只提交了 dist 的一半。
func TestEmbedAssetsReferencedByIndexExist(t *testing.T) {
	body, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(body), -1)
	if len(refs) == 0 {
		t.Fatal("index.html references no /assets/* bundle; dist looks stale or hand-written")
	}
	for _, m := range refs {
		name := strings.TrimPrefix(m[1], "/")
		info, err := fs.Stat(FS(), name)
		if err != nil {
			t.Errorf("index.html references %s but it is not embedded: %v", m[1], err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("embedded asset %s is empty", name)
		}
	}
}

// 离线约束：不得引用公网样式/字体/图标/脚本，也不得内嵌字体文件。
// 允许出现的是 XML 命名空间（w3.org）与纯文本中的示例地址，因此只检查真正会发起请求的位置。
func TestEmbedHasNoExternalResources(t *testing.T) {
	badHosts := []string{
		"fonts.googleapis.com", "fonts.gstatic.com", "cdn.jsdelivr.net",
		"cdnjs.cloudflare.com", "unpkg.com", "cdn.tailwindcss.com",
	}
	// <link href="http(s)://…"> 与 <script src="http(s)://…"> 会真的发起请求。
	remoteLink := regexp.MustCompile(`<(?:link|script)[^>]+(?:href|src)="(?:https?:)?//[^"]*"`)
	// 远程字体的 @import / @font-face src。
	remoteImport := regexp.MustCompile(`@import\s+url\(\s*['"]?(?:https?:)?//`)

	walk := func(path string) {
		data, err := fs.ReadFile(FS(), path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			return
		}
		text := string(data)
		if m := remoteLink.FindString(text); m != "" {
			t.Errorf("%s loads an external resource: %s", path, m)
		}
		if m := remoteImport.FindString(text); m != "" {
			t.Errorf("%s imports an external stylesheet: %s", path, m)
		}
		for _, h := range badHosts {
			if strings.Contains(text, h) {
				t.Errorf("%s references CDN host %s", path, h)
			}
		}
	}

	walk("index.html")
	err := fs.WalkDir(FS(), "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		walk(path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
}

// 字体必须走系统字体栈：不得内嵌字体文件（无 .woff/.woff2/.ttf/.otf）。
func TestEmbedHasNoFontFiles(t *testing.T) {
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch strings.ToLower(path.Ext(p)) {
		case ".woff", ".woff2", ".ttf", ".otf", ".eot":
			t.Errorf("embedded font file found: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded files: %v", err)
	}
}
