// stats.go 限流可观测（docs/tasks-eu-rate-limit.md RL-5）：import_log 已含全部
// 要素（来源、状态码、结果、字节），缺的只是聚合视图。本视图按日汇总欧盟通道
// （europa.eu 主机）请求：请求数、成功/失败、429 率、累计字节，以及按 CELEX 的
// 请求计数——配额决策（RL-1 pilot / RL-4 分批）以此为准，不凭印象。
package ingest

import (
	"regexp"
	"sort"
	"strings"

	"github.com/futuremeng/OpenLaw/store"
)

// EUQuotaDay 一日一行的欧盟通道请求统计。
type EUQuotaDay struct {
	Day        string `json:"day"`
	Requests   int    `json:"requests"`
	OK         int    `json:"ok"`
	Failed     int    `json:"failed"`
	Limited429 int    `json:"limited_429"`
	Bytes      int64  `json:"bytes"`
}

// EUQuotaCelex 按 CELEX 编号的请求计数（配额消耗归因）。
type EUQuotaCelex struct {
	Celex    string `json:"celex"`
	Requests int    `json:"requests"`
	OK       int    `json:"ok"`
}

// EUQuotaReport eu 配额报表。
type EUQuotaReport struct {
	GeneratedAt string         `json:"generated_at"`
	Since       string         `json:"since,omitempty"`
	Days        []EUQuotaDay   `json:"days"`
	Celexes     []EUQuotaCelex `json:"celexes"`
}

// celexURLPattern source_url 里的 CELEX 编号（与 celexPattern 同构，去锚定）。
var celexURLPattern = regexp.MustCompile(`[0-9]{5}[A-Z][0-9]{4}[A-Z0-9]?`)

// EUQuotaStats 聚合 import_log 的欧盟通道请求。since 为空 = 全部历史。
// 只统计 europa.eu 来源（中国/美国通道不混入）。
func EUQuotaStats(db *store.DB, since string) (*EUQuotaReport, error) {
	rows, err := db.Query(`SELECT substr(created_at,1,10), COUNT(*),
		SUM(result='success'), SUM(result='failed'), SUM(COALESCE(http_status,0)=429),
		COALESCE(SUM(file_size),0)
		FROM import_log
		WHERE source_url LIKE '%europa.eu%' AND (? = '' OR created_at >= ?)
		GROUP BY 1 ORDER BY 1`, since, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rep := &EUQuotaReport{}
	for rows.Next() {
		var d EUQuotaDay
		if err := rows.Scan(&d.Day, &d.Requests, &d.OK, &d.Failed, &d.Limited429, &d.Bytes); err != nil {
			return nil, err
		}
		rep.Days = append(rep.Days, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// CELEX 归因：从 source_url 提取编号（探测/全文链的 URI 都带编号）。
	urls, err := db.Query(`SELECT source_url, result FROM import_log
		WHERE source_url LIKE '%europa.eu%' AND (? = '' OR created_at >= ?)`, since, since)
	if err != nil {
		return nil, err
	}
	defer urls.Close()
	type acc struct{ req, ok int }
	counts := map[string]*acc{}
	for urls.Next() {
		var u, result string
		if err := urls.Scan(&u, &result); err != nil {
			return nil, err
		}
		m := celexURLPattern.FindString(u)
		if m == "" {
			continue
		}
		m = strings.ToUpper(m)
		a := counts[m]
		if a == nil {
			a = &acc{}
			counts[m] = a
		}
		a.req++
		if result == "success" {
			a.ok++
		}
	}
	if err := urls.Err(); err != nil {
		return nil, err
	}
	for c, a := range counts {
		rep.Celexes = append(rep.Celexes, EUQuotaCelex{Celex: c, Requests: a.req, OK: a.ok})
	}
	sort.Slice(rep.Celexes, func(i, j int) bool {
		if rep.Celexes[i].Requests != rep.Celexes[j].Requests {
			return rep.Celexes[i].Requests > rep.Celexes[j].Requests
		}
		return rep.Celexes[i].Celex < rep.Celexes[j].Celex
	})
	return rep, nil
}
