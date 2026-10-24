// openlaw-compare：跨来源同一法律文件一致性核验 CLI。
// 用途：核验不同官方来源（如国家法律法规数据库单行 PDF 与国务院公报整期 PDF/HTML）
// 刊载的同一法律文本是否一致。字节级（SHA-256）仅作记录；
// 判定以文本级为准：去空白 + 部首码位折叠后，完全一致或连续包含即为一致。
// 退出码：0=文本一致（含包含）；1=存在实质差异；2=错误。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/futuremeng/OpenLaw/compare"
)

func main() {
	var (
		fileA = flag.String("a", "", "文件 A（.pdf/.html/.htm/.md/.txt），如 flk 单行法 PDF")
		fileB = flag.String("b", "", "文件 B，如公报整期 PDF 或公报单文档 HTML")
		json_ = flag.Bool("json", false, "输出 JSON 报告")
	)
	flag.Parse()
	if *fileA == "" || *fileB == "" {
		fmt.Fprintln(os.Stderr, "用法: openlaw-compare -a FILE -b FILE [-json]")
		os.Exit(2)
	}

	rep, err := compare.CompareFiles(*fileA, *fileB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "核验失败: %v\n", err)
		if strings.Contains(err.Error(), "无文本层") {
			fmt.Fprintln(os.Stderr, "\n该侧为扫描图像版 PDF，文本级核验的替代路径（按推荐顺序）：")
			fmt.Fprintln(os.Stderr, "  1. flk 来源：浏览器打开该法规的在线浏览页（有全文文字），")
			fmt.Fprintln(os.Stderr, "     手动复制/另存为 .txt 放入 data/incoming/，再以其为 -a/-b 参数重跑本命令；")
			fmt.Fprintln(os.Stderr, "  2. gov.cn 公报来源：改用公报单文档 HTML 页（正文在 UCAP-CONTENT 容器，本工具自动提取）；")
			fmt.Fprintln(os.Stderr, "  3. 两侧均为扫描件且无 HTML 文字源时：只能先 OCR（本项目暂未实现），绝不静默入库。")
		}
		os.Exit(2)
	}

	if *json_ {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(os.Stderr, "JSON 输出失败: %v\n", err)
			os.Exit(2)
		}
	} else {
		fmt.Printf("A: %s\nB: %s\n", rep.FileA, rep.FileB)
		fmt.Printf("字节级: SHA-256 %s（%d 字节）\n      %s（%d 字节）→ %v\n",
			rep.SHA256A[:16]+"…", rep.SizeA, rep.SHA256B[:16]+"…", rep.SizeB, rep.BytesIdentical)
		fmt.Printf("文本级: A %d 字 / B %d 字，相似度 %.4f\n", rep.TextLenA, rep.TextLenB, rep.Similarity)
		fmt.Printf("句段覆盖: A→B %d/%d，B→A %d/%d\n", rep.CoverAInB, rep.TotalA, rep.CoverBInA, rep.TotalB)
		switch rep.Verdict {
		case "bytes_identical":
			fmt.Println("结论: 字节级与文本级完全一致")
		case "identical":
			fmt.Println("结论: 文本级一致（排版差异已折叠；字节级不一致属正常，PDF 生成器不同）")
		case "contained":
			fmt.Printf("结论: 文本连续包含（%s）——单行法文本是整期文本的连续子串，视为一致\n", rep.Containment)
		case "covered":
			fmt.Println("结论: 句段全覆盖——正文全部句段在另一侧命中（分页页眉/页码等插入噪声已容忍），视为一致")
		default:
			fmt.Printf("结论: 存在实质差异。首个差异位点 %d，中段差异 A %d 字 / B %d 字\n", rep.FirstDiffPos, rep.DiffLenA, rep.DiffLenB)
			fmt.Printf("  A: %s\n  B: %s\n", rep.ContextA, rep.ContextB)
			for _, m := range rep.Missing {
				fmt.Printf("  未命中句段: %s\n", m)
			}
			fmt.Println("  注意：本工具不折叠标点/繁简/异体差异，剩余差异需人工裁定是否属排版变体。")
		}
	}

	if rep.Consistent() {
		os.Exit(0)
	}
	os.Exit(1)
}
