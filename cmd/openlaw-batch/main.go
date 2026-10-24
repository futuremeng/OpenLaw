// openlaw-batch：从 openlaw-scan 报告 CSV 批量导入存量（用户提供的本地文件，绝不联网）。
// 元数据仅从文件名保守推导：《名称》_YYYYMMDD.docx → law/document/version；
// 效力等级留空（NULL）、生效日期暂按文件名日期填充、confirm_status=pending，全部转人工确认。
// 去重三层：报告内 sha 重复（dup_of 非空）跳过；库内同 sha 跳过；doc_id 冲突由导入器拒绝并记录。
// 逐条失败不中断，产出 batch_report.csv；每条导入均写 import_log（导入器职责）。
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/futuremeng/OpenLaw/ingest"
	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

var nameDateRe = regexp.MustCompile(`^(.+?)_(\d{8})$`)

func main() {
	var (
		data   = flag.String("data", "data", "数据目录")
		report = flag.String("report", "", "openlaw-scan 报告 CSV（必填）")
		outCSV = flag.String("out", "", "批量导入报告（默认 <report 同目录>/batch_report.csv）")
		ext    = flag.String("ext", ".docx", "只导入该扩展名")
		limit  = flag.Int("limit", 0, "只处理前 N 条（0=全部，试跑用）")
	)
	flag.Parse()
	if *report == "" {
		fmt.Fprintln(os.Stderr, "错误: -report 必填")
		os.Exit(2)
	}
	if *outCSV == "" {
		*outCSV = filepath.Join(filepath.Dir(*report), "batch_report.csv")
	}
	f, err := os.Open(*report)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		fatal(fmt.Errorf("读取报告失败: %w", err))
	}
	if len(rows) < 2 || strings.Join(rows[0], ",") != "zip,entry,out,ext,size,sha256,text,dup_of" {
		fatal(fmt.Errorf("报告格式不符（需 openlaw-scan 产出，含表头）"))
	}

	if err := os.MkdirAll(filepath.Join(*data, "db"), 0o755); err != nil {
		fatal(err)
	}
	db, err := store.Open(filepath.Join(*data, "db", "openlaw.db"))
	if err != nil {
		fatal(err)
	}
	defer db.Close()
	im := &ingest.Importer{DataDir: *data, DB: db}

	out, err := os.Create(*outCSV)
	if err != nil {
		fatal(err)
	}
	defer out.Close()
	w := csv.NewWriter(out)
	defer w.Flush()
	_ = w.Write([]string{"doc_id", "law_id", "name", "date", "status", "articles", "detail"})

	cnt := map[string]int{}
	done := 0
	for _, r := range rows[1:] {
		if *limit > 0 && done >= *limit {
			break
		}
		zipF, entry, outPath, rowExt, sha, text, dupOf := r[0], r[1], r[2], r[3], r[5], r[6], r[7]
		_ = zipF
		status, artN, detail := "ok", 0, ""
		docID, lawID, dateISO := "", "", ""
		notes := ""
		switch {
		case rowExt != *ext:
			status, detail = "skip_ext", ""
		case text != "yes":
			status, detail = "skip_no_text", ""
		case dupOf != "":
			status, detail = "skip_dup_report", dupOf
		case outPath == "" || strings.HasPrefix(outPath, "ERROR"):
			status, detail = "skip_no_file", outPath
		default:
			base := strings.TrimSuffix(filepath.Base(entry), filepath.Ext(entry))
			var name, pubISO string
			inferred := true // 批量通道日期一律 inferred（文件名/正文落款皆非官方元数据直取）
			m := nameDateRe.FindStringSubmatch(base)
			if m != nil {
				d8 := m[2]
				name = m[1]
				dateISO = d8[0:4] + "-" + d8[4:6] + "-" + d8[6:8]
				pubISO, notes = dateISO, "batch：生效日期暂按文件名日期填充、效力等级待人工确认"
			} else {
				// 兜底：正文落款保守提取日期（parse.InferDates；无命中仍 skip 转人工）
				txt, xerr := parse.ExtractDocxText(outPath)
				pub, eff, ok := "", "", false
				if xerr == nil {
					pub, eff, ok = parse.InferDates(txt)
				}
				if !ok {
					status, detail = "skip_no_date", base
					if xerr != nil {
						detail = base + " | " + xerr.Error()
					}
					break
				}
				name, dateISO, pubISO = base, eff, pub
				if pubISO == "" {
					pubISO = eff
				}
				notes = "batch：日期取自正文落款提取（inferred）、效力等级待人工确认"
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM document WHERE sha256 = ?`, sha).Scan(&n); err != nil {
				status, detail = "fail", err.Error()
				break
			}
			if n > 0 {
				status, detail = "skip_dup_db", sha
				break
			}
			sum := sha256.Sum256([]byte(name))
			lawID = fmt.Sprintf("flk_%x", sum[:6])
			docID = lawID + "_" + strings.Replace(dateISO, "-", "", -1)
			res, ierr := im.Import(ingest.Request{
				DocID: docID, LawID: lawID, LawName: name,
				VersionNo: dateISO, EffectiveDate: dateISO, PublishDate: pubISO,
				SourceType: "flk 批量下载（用户提供存量）", ConfirmStatus: "pending",
				Notes: notes, EffectiveDateInferred: inferred,
				PDFPath: outPath, AutoExpirePrior: true,
			})
			if ierr != nil {
				status, detail = "fail", ierr.Error()
			} else {
				artN = len(res.ArticleIDs)
				status = "ok"
				if m == nil {
					status = "ok_inferred"
					detail = "date from text: pub=" + pubISO + " eff=" + dateISO
				}
			}
		}
		cnt[status]++
		done++
		if err := w.Write([]string{docID, lawID, entry, dateISO, status, fmt.Sprint(artN), detail}); err != nil {
			fatal(err)
		}
		if done%200 == 0 {
			w.Flush()
			fmt.Fprintf(os.Stderr, "进度: %d 条 %v\n", done, cnt)
		}
	}
	w.Flush()
	fmt.Printf("批量导入完成: 共 %d 条 %v\n报告: %s\n", done, cnt, *outCSV)
	if cnt["fail"] > 0 {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(2)
}
