// coverage.go 复核兑现进度（产品评审 P0-1）。
//
// 产品的核心承诺是「可溯源 + 已复核」，但首发语料 100% 待复核——机制完备而内容未兑现。
// 本文件把这件事变成两个可核对的数字：**常用法规复核了多少**、**全库复核了多少**，
// 并如实列出还缺哪些（缺就是缺，不得用「基本覆盖」这类模糊话）。
//
// 口径纪律：
//   - 「已复核」只认 document.confirm_status='confirmed'（智能体确认与人工确认都算已复核，
//     来源区分由智能审核面板负责，这里不重复统计口径）；
//   - 命中按**全称 → 简称列 → 别名表**三级，与 sdk 法名解析同一顺序；别名表不存在时
//     按「无别名」如实处理，不因此把已收录的法规报成缺失；
//   - 未收录与未复核清单一次最多列 limit 条，其余以计数说明（诚实：不得截断后说「全部」）。
package ingest

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// Coverage 复核兑现进度。
type Coverage struct {
	PriorityTotal      int      `json:"priority_total"`
	PriorityInCorpus   int      `json:"priority_in_corpus"`
	PriorityConfirmed  int      `json:"priority_confirmed"`
	PriorityUnreviewed int      `json:"priority_unreviewed"` // 已收录但没有任何已复核文本
	PriorityMissing    int      `json:"priority_missing"`    // 库内完全没有
	MissingNames       []string `json:"missing_names,omitempty"`
	UnreviewedNames    []string `json:"unreviewed_names,omitempty"`
	Truncated          bool     `json:"truncated"` // 清单被截断（其余只给计数）

	CorpusLaws      int     `json:"corpus_laws"`
	CorpusDocs      int     `json:"corpus_docs"`
	CorpusConfirmed int     `json:"corpus_confirmed"`
	ConfirmedPct    float64 `json:"confirmed_pct"`

	ListNote string `json:"list_note"`
}

// lawIndex 库内法规的命中索引（全称/简称/别名 → 该法是否已有已复核文本）。
type lawIndex struct {
	confirmed bool
	docs      int
}

// normalizeLawName 名称比对归一：去空白与书名号（清单与库内写法差异只应来自排版）。
func normalizeLawName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("《", "", "》", "", " ", "", "\t", "", "\u3000", "", "\n", "").Replace(s)
	return s
}

// ReviewCoverage 计算复核兑现进度。limit<=0 时清单不列明细，只给计数。
func ReviewCoverage(db *store.DB, limit int) (*Coverage, error) {
	if db == nil {
		return nil, fmt.Errorf("法规库未打开")
	}
	c := &Coverage{PriorityTotal: len(PriorityLaws), ListNote: PriorityListNote}

	index := map[string]lawIndex{}
	rows, err := db.Query(`SELECT l.law_id, l.name, COALESCE(l.short_name,''),
		(SELECT COUNT(*) FROM law_version v JOIN document d ON d.doc_id=v.doc_id WHERE v.law_id=l.law_id),
		(SELECT SUM(d.confirm_status='confirmed') FROM law_version v JOIN document d ON d.doc_id=v.doc_id WHERE v.law_id=l.law_id)
		FROM law l`)
	if err != nil {
		return nil, fmt.Errorf("读取法规索引失败: %w", err)
	}
	for rows.Next() {
		var id, name, short string
		var docs, conf sql.NullInt64
		if err := rows.Scan(&id, &name, &short, &docs, &conf); err != nil {
			rows.Close()
			return nil, err
		}
		li := lawIndex{confirmed: conf.Int64 > 0, docs: int(docs.Int64)}
		if n := normalizeLawName(name); n != "" {
			index[n] = li
		}
		if n := normalizeLawName(short); n != "" {
			if cur, ok := index[n]; !ok || (li.confirmed && !cur.confirmed) {
				index[n] = li
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 别名表（v9）：只用于命中，不改判定。表不存在（v8 及更早语料）按「无别名」处理。
	// 注意：必须先把别名读尽再逐条查已复核状态——SQLite 单连接下
	// 在游标迭代中再发起查询会自锁（曾导致整页卡死）。
	var aliasN int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='law_alias'`).Scan(&aliasN); err == nil && aliasN > 0 {
		type aliasRow struct{ alias, lawID string }
		var pairs []aliasRow
		if arows, err := db.Query(`SELECT a.alias, a.law_id FROM law_alias a`); err == nil {
			for arows.Next() {
				var alias, lawID string
				if err := arows.Scan(&alias, &lawID); err == nil {
					pairs = append(pairs, aliasRow{alias, lawID})
				}
			}
			arows.Close()
			for _, p := range pairs {
				n := normalizeLawName(p.alias)
				if n == "" {
					continue
				}
				if _, ok := index[n]; !ok {
					index[n] = lawIndex{confirmed: lawConfirmed(db, p.lawID)}
				}
			}
		}
	}

	for _, p := range PriorityLaws {
		li, ok := index[normalizeLawName(p.Name)]
		if !ok {
			c.PriorityMissing++
			if len(c.MissingNames) < limit {
				c.MissingNames = append(c.MissingNames, p.Name)
			} else if limit > 0 {
				c.Truncated = true
			}
			continue
		}
		c.PriorityInCorpus++
		if li.confirmed {
			c.PriorityConfirmed++
			continue
		}
		c.PriorityUnreviewed++
		if len(c.UnreviewedNames) < limit {
			c.UnreviewedNames = append(c.UnreviewedNames, p.Name)
		} else if limit > 0 {
			c.Truncated = true
		}
	}

	_ = db.QueryRow(`SELECT COUNT(*) FROM law`).Scan(&c.CorpusLaws)
	_ = db.QueryRow(`SELECT COUNT(*) FROM document`).Scan(&c.CorpusDocs)
	_ = db.QueryRow(`SELECT COUNT(*) FROM document WHERE confirm_status='confirmed'`).Scan(&c.CorpusConfirmed)
	if c.CorpusDocs > 0 {
		c.ConfirmedPct = float64(c.CorpusConfirmed) * 100 / float64(c.CorpusDocs)
	}
	return c, nil
}

// lawConfirmed 该法规是否已有已复核确认的文本（别名命中路径用）。
func lawConfirmed(db *store.DB, lawID string) bool {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM law_version v JOIN document d ON d.doc_id=v.doc_id
		WHERE v.law_id=? AND d.confirm_status='confirmed'`, lawID).Scan(&n)
	return n > 0
}

// Headline 库总览用的一行白话（缺失与未复核必须直说，不得渲染成中性数字）。
func (c *Coverage) Headline() string {
	if c.CorpusDocs == 0 {
		return "法规库尚无文本，谈不上复核进度——请先获取或导入数据。"
	}
	if c.CorpusConfirmed == 0 {
		return fmt.Sprintf("本机 %s 条文本尚未经人工复核确认，引用前请以官方原文为准。", itoa(c.CorpusDocs))
	}
	return fmt.Sprintf("已复核确认 %s / %s 条（%.1f%%）；常用法规参考清单 %s 部中已收录 %s 部、已复核 %s 部。",
		itoa(c.CorpusConfirmed), itoa(c.CorpusDocs), c.ConfirmedPct,
		itoa(c.PriorityTotal), itoa(c.PriorityInCorpus), itoa(c.PriorityConfirmed))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
