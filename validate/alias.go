// 法名别名表构建（OL-4 写路径）。扫描条文文本中的「以下简称」条款，把「简称 → 法规」
// 落进 law_alias 表。纪律：
//   - 全称必须能在 law 表**精确反查**（书名号与空白规范化后比较）；反查不到如实不收录，
//     并在报告里给出样本——绝不凭文本臆造法规身份；
//   - 一条文本抽出多对时逐对处理；同名法规多部时逐条收录（解析层因此报多候选交人工）；
//   - law.short_name 一并镜像进表，使别名表成为可巡检的唯一入口；
//   - 默认 dry-run（只出计数与 CSV 报告），-apply 才写库；写入幂等（同 alias+law_id 更新）。
package validate

import (
	"encoding/csv"
	"os"
	"time"

	"github.com/futuremeng/OpenLaw/parse"
	"github.com/futuremeng/OpenLaw/store"
)

// AliasRow 一条别名收录记录（CSV 报告行；Status 如实说明「将会」口径）。
type AliasRow struct {
	Alias     string `json:"alias"`
	LawID     string `json:"law_id"`
	LawName   string `json:"law_name"`
	Source    string `json:"source"`
	ArticleID string `json:"article_id"`
	Status    string `json:"status"` // will_add | will_update | unresolved_full_name | error:*
}

// AliasBuildReport 别名表构建报告。Applied=false 表示 dry-run（计数为「将会」口径）。
type AliasBuildReport struct {
	ArticlesScanned int        `json:"articles_scanned"`
	Extracted       int        `json:"extracted"`
	Resolved        int        `json:"resolved"`
	Unresolved      int        `json:"unresolved"` // 全称在法规库中查不到，如实不收录
	Added           int        `json:"added"`
	Updated         int        `json:"updated"`
	Duplicates      int        `json:"duplicates"` // 同一批次内重复的 (alias, law_id)：只落一行
	Rows            []AliasRow `json:"rows,omitempty"`
	Applied         bool       `json:"applied"`

	seen map[string]bool // 本批次已见过的 (alias, law_id) → 是否已带出处条文（保证计数诚实与依据不被覆盖）
}

// BuildLawAliases 构建/刷新法名别名表。apply=false 时绝不写库。
// 连接池为单连接（store.Open SetMaxOpenConns(1)），因此必须**先把游标读完并关闭**，
// 再逐条反查/写入——在游标未关闭时发起嵌套查询会自锁（实测会挂住）。
func BuildLawAliases(db *store.DB, apply bool) (*AliasBuildReport, error) {
	rep := &AliasBuildReport{}
	now := time.Now().UTC().Format(time.RFC3339)

	// 1) law.short_name 镜像（来源可溯源：表内既有简称列）
	type lawRow struct{ lawID, name, short string }
	var laws []lawRow
	srows, err := db.Query(`SELECT law_id, name, short_name FROM law
		WHERE short_name IS NOT NULL AND short_name <> '' ORDER BY law_id`)
	if err != nil {
		return nil, err
	}
	for srows.Next() {
		var r lawRow
		if err := srows.Scan(&r.lawID, &r.name, &r.short); err != nil {
			srows.Close()
			return nil, err
		}
		laws = append(laws, r)
	}
	if err := srows.Err(); err != nil {
		srows.Close()
		return nil, err
	}
	srows.Close()
	for _, r := range laws {
		rep.collect(db, apply, AliasRow{Alias: NormalizeLawName(r.short), LawID: r.lawID,
			LawName: r.name, Source: AliasSourceShortName}, now)
	}

	// 2) 条文文本中的「以下简称」条款（全称必须反查到 law.name）
	var articleIDs []string
	ids, err := db.Query(`SELECT article_id FROM article WHERE content LIKE '%以下简称%' ORDER BY article_id`)
	if err != nil {
		return nil, err
	}
	for ids.Next() {
		var id string
		if err := ids.Scan(&id); err != nil {
			ids.Close()
			return nil, err
		}
		articleIDs = append(articleIDs, id)
	}
	if err := ids.Err(); err != nil {
		ids.Close()
		return nil, err
	}
	ids.Close()

	for _, articleID := range articleIDs {
		var content string
		if err := db.QueryRow(`SELECT content FROM article WHERE article_id = ?`, articleID).Scan(&content); err != nil {
			return nil, err
		}
		rep.ArticlesScanned++
		for _, p := range parse.ExtractLawShortNames(content) {
			rep.Extracted++
			alias := NormalizeLawName(p.ShortName)
			cands, err := lawRowsBy(db, "name", []string{NormalizeLawName(p.FullName)})
			if err != nil {
				return nil, err
			}
			if len(cands) == 0 {
				// 全称不在法规库里：这条简称无法确定法规身份，如实不收录
				rep.Unresolved++
				rep.Rows = append(rep.Rows, AliasRow{Alias: alias, LawName: p.FullName,
					Source: AliasSourceText, ArticleID: articleID, Status: "unresolved_full_name"})
				continue
			}
			for _, c := range cands {
				rep.Resolved++
				rep.collect(db, apply, AliasRow{Alias: alias, LawID: c.LawID, LawName: c.Name,
					Source: AliasSourceText, ArticleID: articleID}, now)
			}
		}
	}
	rep.Applied = apply
	return rep, nil
}

// collect 计数并（仅 apply 时）写入一条别名；同 alias+law_id 记「更新」而非重复新增。
// 同一批次内重复命中同一 (alias, law_id)（简称列与文本条款都给出同一别名）只算一行，
// 如实计入 Duplicates——dry-run 的「将会新增」计数不得虚高。
func (rep *AliasBuildReport) collect(db *store.DB, apply bool, row AliasRow, now string) {
	if row.Alias == "" || row.LawID == "" {
		return
	}
	if rep.seen == nil {
		rep.seen = map[string]bool{}
	}
	key := row.Alias + "\x00" + row.LawID
	if done, seen := rep.seen[key]; seen {
		rep.Duplicates++
		row.Status = "dup_in_batch"
		rep.Rows = append(rep.Rows, row)
		// 同一行被两条来源命中：带出处条文的一条（corpus-text）胜出，依据不得被镜像覆盖
		if apply && !done && row.ArticleID != "" {
			rep.upsert(db, row, now)
		}
		return
	}
	rep.seen[key] = row.ArticleID != ""
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias WHERE alias=? AND law_id=?`,
		row.Alias, row.LawID).Scan(&n); err != nil {
		row.Status = "error: " + err.Error()
		rep.Rows = append(rep.Rows, row)
		return
	}
	if n > 0 {
		row.Status = "will_update"
		rep.Updated++
	} else {
		row.Status = "will_add"
		rep.Added++
	}
	rep.Rows = append(rep.Rows, row)
	if !apply {
		return
	}
	rep.upsert(db, row, now)
}

// upsert 写入一行别名；已存在则整行刷新（幂等，重复构建不增行）。
func (rep *AliasBuildReport) upsert(db *store.DB, row AliasRow, now string) {
	if _, err := db.Exec(`INSERT INTO law_alias(alias, law_id, source, source_article_id, built_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(alias, law_id) DO UPDATE SET source=excluded.source,
			source_article_id=excluded.source_article_id, built_at=excluded.built_at`,
		row.Alias, row.LawID, row.Source, row.ArticleID, now); err != nil {
		rep.Rows = append(rep.Rows, AliasRow{Alias: row.Alias, LawID: row.LawID,
			Source: row.Source, ArticleID: row.ArticleID, Status: "error: " + err.Error()})
	}
}

// WriteAliasReport 把构建报告落成 CSV（每别名一行，含未收录样本，供人工巡检）。
func WriteAliasReport(path string, rep *AliasBuildReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"alias", "law_id", "law_name", "source", "article_id", "status"}); err != nil {
		return err
	}
	for _, r := range rep.Rows {
		if err := w.Write([]string{r.Alias, r.LawID, r.LawName, r.Source, r.ArticleID, r.Status}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// AliasStats 别名表规模与来源分布（只读；表不存在时 Exists=false，如实报缺失）。
type AliasStats struct {
	Exists  bool           `json:"exists"`
	Total   int            `json:"total"`
	Sources map[string]int `json:"sources"`
	Samples []AliasRow     `json:"samples,omitempty"`
}

// LawAliasStats 统计别名表（报表不新增任何写入，也不联网）。
func LawAliasStats(db *store.DB) (*AliasStats, error) {
	st := &AliasStats{Sources: map[string]int{}}
	if !aliasTableAvailable(db) {
		return st, nil
	}
	st.Exists = true
	if err := db.QueryRow(`SELECT COUNT(*) FROM law_alias`).Scan(&st.Total); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT source, COUNT(*) FROM law_alias GROUP BY source ORDER BY source`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			rows.Close()
			return nil, err
		}
		st.Sources[s] = n
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = db.Query(`SELECT a.alias, a.law_id, l.name, a.source, COALESCE(a.source_article_id,'')
		FROM law_alias a JOIN law l ON l.law_id=a.law_id ORDER BY a.alias LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r AliasRow
		if err := rows.Scan(&r.Alias, &r.LawID, &r.LawName, &r.Source, &r.ArticleID); err != nil {
			return nil, err
		}
		st.Samples = append(st.Samples, r)
	}
	return st, rows.Err()
}
