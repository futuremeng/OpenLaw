// openlaw-scan：存量批量下载包盘点与分类（只读源 zip，可选解压到指定目录）。
// 适用：flk 站内批量下载 zip（GBK 文件名、.docx/.pdf/.txt 混合）等本地存量。
// 产出 CSV 报告（zip,entry,out,ext,size,sha256,text,dup_of）供人工审阅与后续批量导入；
// 不写 SQLite——盘点层与导入层分离；SHA-256 在此即固定，导入时复用判篡改。
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/futuremeng/OpenLaw/parse"
)

func main() {
	var (
		src     = flag.String("src", "", "存量目录（顶层 *.zip，必填）")
		out     = flag.String("out", "", "报告 CSV 路径（默认 <src>/scan_report.csv）")
		extract = flag.String("extract", "", "解压目标目录（留空=只盘点不解压）")
		limit   = flag.Int("limit", 0, "只处理前 N 个 zip（0=全部，试跑用）")
		jobs    = flag.Int("jobs", 4, "并行处理的 zip 数")
	)
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "错误: -src 必填")
		os.Exit(2)
	}
	zips, err := filepath.Glob(filepath.Join(*src, "*.zip"))
	if err != nil || len(zips) == 0 {
		fmt.Fprintf(os.Stderr, "错误: %s 下未找到 *.zip: %v\n", *src, err)
		os.Exit(2)
	}
	sort.Strings(zips)
	if *limit > 0 && *limit < len(zips) {
		zips = zips[:*limit]
	}
	if *out == "" {
		*out = filepath.Join(*src, "scan_report.csv")
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(2)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"zip", "entry", "out", "ext", "size", "sha256", "text", "dup_of"})

	ctx := &scanCtx{w: w, seen: map[string]string{}, byExt: map[string]int{}}
	var wg sync.WaitGroup
	ch := make(chan string)
	njobs := *jobs
	if njobs < 1 {
		njobs = 1
	}
	for j := 0; j < njobs; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for zp := range ch {
				if err := scanZip(zp, *extract, ctx); err != nil {
					fmt.Fprintf(os.Stderr, "警告: 跳过 %s: %v\n", filepath.Base(zp), err)
				}
				n := ctx.doneInc()
				fmt.Fprintf(os.Stderr, "进度: %d/%d %s\n", n, len(zips), filepath.Base(zp))
				ctx.flush()
			}
		}()
	}
	for _, zp := range zips {
		ch <- zp
	}
	close(ch)
	wg.Wait()
	ctx.flush()
	agg := ctx.agg
	fmt.Printf("盘点完成: zip=%d 文件=%d 重复=%d 文本=%d 无文本/扫描=%d 未识别=%d 错误=%d\n",
		len(zips), agg.files, agg.dup, agg.yes, agg.no, agg.na, agg.errs)
	for ext, n := range ctx.byExt {
		fmt.Printf("  %-6s %d\n", ext, n)
	}
	fmt.Println("报告:", *out)
}

// scanCtx 并行共享状态：CSV、去重表、计数（互斥保护）。
type scanCtx struct {
	mu    sync.Mutex
	w     *csv.Writer
	seen  map[string]string
	byExt map[string]int
	agg   struct{ files, dup, yes, no, na, errs int }
	done  int
}

func (c *scanCtx) doneInc() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done++
	return c.done
}
func (c *scanCtx) flush() { c.mu.Lock(); c.w.Flush(); c.mu.Unlock() }

// scanZip 盘点单个 zip：GBK 文件名解码 → 哈希 → 可选解压 → 文本探测 → CSV 行。
// 单条目失败（如写盘错误）不中断：记 out=ERROR:... 并继续（errs 计数）。
func scanZip(zp, extractDir string, ctx *scanCtx) error {
	zr, err := zip.OpenReader(zp)
	if err != nil {
		return err
	}
	defer zr.Close()
	zipBase := strings.TrimSuffix(filepath.Base(zp), ".zip")
	for _, fh := range zr.File {
		if fh.FileHeader.FileInfo().IsDir() {
			continue
		}
		name := decodeName(fh.Name, fh.NonUTF8)
		name = sanitize(name)
		ext := strings.ToLower(filepath.Ext(name))
		rc, err := fh.Open()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		rc.Close()
		if err != nil {
			return fmt.Errorf("%s: 读取失败: %w", name, err)
		}
		sum := sha256.Sum256(data)
		sha := hex.EncodeToString(sum[:])
		outPath := ""
		if extractDir != "" {
			outPath, err = writeFile(filepath.Join(extractDir, zipBase, name), data)
			if err != nil {
				outPath = "ERROR:" + err.Error()
			}
		}
		text := textOK(ext, data, outPath)
		dupOf := ""
		ctx.mu.Lock()
		if first, ok := ctx.seen[sha]; ok {
			dupOf = first
			ctx.agg.dup++
		} else {
			ctx.seen[sha] = name
		}
		ctx.agg.files++
		switch text {
		case "yes":
			ctx.agg.yes++
		case "no":
			ctx.agg.no++
		default:
			ctx.agg.na++
		}
		if strings.HasPrefix(outPath, "ERROR:") {
			ctx.agg.errs++
		}
		ctx.byExt[ext]++
		cerr := ctx.w.Write([]string{filepath.Base(zp), fh.Name, outPath, ext,
			fmt.Sprint(len(data)), sha, text, dupOf})
		ctx.mu.Unlock()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

// decodeName zip 条日名解码：非 UTF-8 标记（flk 等中文站点常见）按 GBK 转。
func decodeName(raw string, nonUTF8 bool) string {
	if !nonUTF8 && utf8.ValidString(raw) {
		return raw
	}
	dec, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(raw))
	if err != nil {
		return raw
	}
	return string(dec)
}

// sanitize 只保留基名并清除路径分隔与控制字符（zip 条目防穿越）；
// 超长名在 rune 边界截断至 200 字节（保留扩展名）——APFS/ext4 单段名上限 255 字节，
// flk 存在超长法规名（如澳门基本法附件决定），不截断会整条写入失败。
func sanitize(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if s == "" || s == "." || s == ".." {
		return "unnamed"
	}
	if len(s) > 200 {
		ext := filepath.Ext(s)
		if len(ext) > 8 {
			ext = ""
		}
		var t strings.Builder
		for _, r := range strings.TrimSuffix(s, ext) {
			if t.Len()+len(string(r)) > 190-len(ext) {
				break
			}
			t.WriteRune(r)
		}
		s = t.String() + ext
	}
	return s
}

// writeFile 防覆盖落盘（同名追加 -1/-2），返回实际路径。
func writeFile(path string, data []byte) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	p := path
	for i := 1; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		p = fmt.Sprintf("%s-%d%s", path[:len(path)-len(filepath.Ext(path))], i, filepath.Ext(path))
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// textOK 文本可用性探测：docx 取 <w:t> 正文；pdf 走文本层提取；纯文本类直接看内容。
// 返回 yes / no（扫描或空）/ n/a（未识别类型）。
func textOK(ext string, data []byte, extractedPath string) string {
	switch ext {
	case ".docx":
		return docxTextOK(data)
	case ".pdf":
		p := extractedPath
		if p == "" || strings.HasPrefix(p, "ERROR:") {
			tmp, err := os.CreateTemp("", "scan-*.pdf")
			if err != nil {
				return "n/a"
			}
			tmp.Write(data)
			tmp.Close()
			defer os.Remove(tmp.Name())
			p = tmp.Name()
		}
		if _, err := parse.ExtractPDFText(p); err != nil {
			return "no"
		}
		return "yes"
	case ".txt", ".md", ".htm", ".html":
		if len(strings.TrimSpace(string(data))) > 0 {
			return "yes"
		}
		return "no"
	default:
		return "n/a"
	}
}

var wtRe = regexp.MustCompile(`(?s)<w:t[^>]*>(.*?)</w:t>`)

// docxTextOK docx = zip 容器：读 word/document.xml，统计 <w:t> 正文字符数。
func docxTextOK(data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "n/a"
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "n/a"
		}
		xml, err := io.ReadAll(io.LimitReader(rc, 32<<20))
		rc.Close()
		if err != nil {
			return "n/a"
		}
		n := 0
		for _, m := range wtRe.FindAllStringSubmatch(string(xml), -1) {
			n += len(strings.TrimSpace(m[1]))
		}
		if n >= 20 {
			return "yes"
		}
		return "no"
	}
	return "n/a"
}
