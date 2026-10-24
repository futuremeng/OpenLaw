package ingest

// 欧盟 Publications Office Cellar 元数据客户端（docs/sources-eu.md，2026-10 实网核实）。
// 合规前提（与 flk 相反，Cellar 是官方开放的程序化入口）：
//   - robots.txt（op.europa.eu）User-agent: * Allow: /，/webapi/rdf/sparql 不在禁止之列；
//   - 仅显式触发（openlaw-eu），默认离线；每次查询间隔 MinInterval 礼貌限速（站点限流严，连发即 429）；
//   - 每次 SPARQL 访问写 import_log；原始三元组 JSON 落 data/incoming/ 留证；
//   - 只写 law 主表与 pending 关系，不建 document/law_version（全文文本尚未获取，
//     EUR-Lex 网页有 WAF 挑战，全文须人工保存或待 Cellar 表示层语法实现）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// CellarBase Cellar 端点所在主机（白名单默认值）。
const CellarBase = "https://publications.europa.eu"

// cellarResBase Cellar 资源规范 URI 前缀（Linked Data 惯例为 http scheme，
// 图内 owl:sameAs 存的是 http://，查询必须按 http 匹配，否则查不中）。
const cellarResBase = "http://publications.europa.eu/resource"

// CellarSPARQL SPARQL 查询端点。
const CellarSPARQL = CellarBase + "/webapi/rdf/sparql"

// celexPattern CELEX 编号：1 位机构 + 4 位年份 + 1 位类型 + 4 位序号（+可选尾字符）。
var celexPattern = regexp.MustCompile(`^[0-9]{5}[A-Z][0-9]{4}[A-Z0-9]?$`)

// Cellar Cellar 元数据客户端。
type Cellar struct {
	DataDir     string
	DB          *store.DB
	Policy      FetchPolicy
	MinInterval time.Duration // 查询间礼貌限速，默认 2s
	Endpoint    string        // SPARQL 端点；空 = CellarSPARQL（仅测试注入其他值）
	robots      *RobotsCache
	lastQuery   time.Time
}

// endpoint 返回生效的 SPARQL 端点。
func (c *Cellar) endpoint() string {
	if c.Endpoint != "" {
		return c.Endpoint
	}
	return CellarSPARQL
}

// EUWork 一部欧盟法律的 work 级元数据（CDM，来自 work named graph）。
type EUWork struct {
	Celex          string   `json:"celex"`
	ELI            string   `json:"eli"`
	GraphURI       string   `json:"graph_uri"`
	TypeLetter     string   `json:"type_letter"` // R=条例 L=指令 D=决定 S=ECSC 决定
	Year           string   `json:"year"`
	Number         string   `json:"number"`
	InForce        bool     `json:"in_force"`
	EntryIntoForce []string `json:"entry_into_force"` // 可能两个：生效日 + 适用日
	EndOfValidity  string   `json:"end_of_validity"`  // 9999-12-31 = 未失效
	Created        string   `json:"created"`
	Signed         string   `json:"signed"`
	OJRef          string   `json:"oj_ref"`          // 官方公报出处（work_id_document oj:…）
	Repeals        []string `json:"repeals"`         // 明示废止（cellar URI）
	ImplicitRepeal []string `json:"implicit_repeal"` // 默示废止（cellar URI）
	BasedOnLegal   []string `json:"based_on_legal"`  // 法律依据（其他法规 cellar URI）
	Treaties       []string `json:"treaties"`        // 依据条约（authority URI）
	FetchedAt      string   `json:"fetched_at"`
}

// sparqlBinding SPARQL JSON 结果的一条绑定。
type sparqlBinding map[string]struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// sparqlResults SPARQL application/sparql-results+json 顶层结构。
type sparqlResults struct {
	Results struct {
		Bindings []sparqlBinding `json:"bindings"`
	} `json:"results"`
}

// sparql 执行一次只读 SELECT（GET），robots 强制 + 限速 + 大小上限。
func (c *Cellar) sparql(ctx context.Context, query string) ([]sparqlBinding, error) {
	if c.MinInterval == 0 {
		c.MinInterval = 2 * time.Second
	}
	if d := time.Since(c.lastQuery); d < c.MinInterval {
		select {
		case <-time.After(c.MinInterval - d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c.lastQuery = time.Now()

	endpoint := c.endpoint() + "?query=" + url.QueryEscape(query) +
		"&format=" + url.QueryEscape("application/sparql-results+json")
	pol := c.policy()

	// robots 强制（与 PDF 抓取同一引擎，fail-closed）
	if c.robots == nil {
		c.robots = NewRobotsCache(pol)
	}
	if ok, why := c.robots.Allowed(ctx, c.endpoint()); !ok {
		c.logQuery(endpoint, "failed", fmt.Sprintf("robots 策略拒绝: %s", why))
		return nil, fmt.Errorf("robots 策略拒绝: %s", why)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pol.UserAgent)
	req.Header.Set("Accept", "application/sparql-results+json")
	client := &http.Client{Timeout: pol.Timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("重定向次数过多")
		}
		if req.URL.Hostname() != mustParseHost(c.endpoint()) {
			return fmt.Errorf("SPARQL 重定向离开 Cellar 主机，拒绝: %s", req.URL.Host)
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		c.logQuery(endpoint, "failed", fmt.Sprintf("请求失败: %v", err))
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("HTTP %d: %.200s", resp.StatusCode, string(body))
		c.logQuery(endpoint, "failed", reason)
		return nil, fmt.Errorf("SPARQL 查询失败: %s", reason)
	}
	if err != nil {
		c.logQuery(endpoint, "failed", err.Error())
		return nil, err
	}
	c.logQuery(endpoint, "success", "")
	var res sparqlResults
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("解析 SPARQL 结果失败: %w", err)
	}
	return res.Results.Bindings, nil
}

// FetchWork 按 CELEX 编号获取 work 级元数据：sameAs 定位 named graph → 读取全部字段
// → 原始三元组 JSON 落 data/incoming/eu_<celex>_work.json 留证。
func (c *Cellar) FetchWork(ctx context.Context, celex string) (*EUWork, error) {
	celex = strings.ToUpper(strings.TrimSpace(celex))
	if !celexPattern.MatchString(celex) {
		return nil, fmt.Errorf("CELEX 编号格式非法: %q（示例 32016R0679）", celex)
	}
	celexURI := cellarResBase + "/celex/" + celex

	// 1) sameAs → work named graph
	rows, err := c.sparql(ctx, fmt.Sprintf(
		"SELECT ?g WHERE { GRAPH ?g { ?s <http://www.w3.org/2002/07/owl#sameAs> <%s> } } LIMIT 3", celexURI))
	if err != nil {
		return nil, err
	}
	graph := ""
	for _, b := range rows {
		if g := b["g"]; strings.HasPrefix(g.Value, cellarResBase+"/cellar/") {
			graph = g.Value
			break
		}
	}
	if graph == "" {
		return nil, fmt.Errorf("Cellar 中未找到 CELEX %s 对应的 work graph（编号是否存在？）", celex)
	}

	// 2) work 级全部三元组
	rows, err = c.sparql(ctx, fmt.Sprintf(
		"SELECT ?p ?o WHERE { GRAPH <%s> { ?s ?p ?o FILTER(str(?s)=<%s>) } } LIMIT 300", graph, graph))
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("work graph %s 无三元组", graph)
	}

	w := &EUWork{Celex: celex, GraphURI: graph, FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, b := range rows {
		p := strings.TrimPrefix(b["p"].Value, "http://publications.europa.eu/ontology/cdm#")
		o := b["o"]
		switch p {
		case "resource_legal_eli":
			w.ELI = o.Value
		case "resource_legal_id_celex":
			if o.Value != "" {
				w.Celex = o.Value
			}
		case "resource_legal_type":
			w.TypeLetter = o.Value
		case "resource_legal_year":
			w.Year = o.Value
		case "resource_legal_number_natural":
			w.Number = o.Value
		case "resource_legal_in-force":
			w.InForce = o.Value == "1"
		case "resource_legal_date_entry-into-force":
			w.EntryIntoForce = append(w.EntryIntoForce, o.Value)
		case "resource_legal_date_end-of-validity":
			w.EndOfValidity = o.Value
		case "resource_legal_date_creation_legacy", "work_date_creation_legacy":
			if w.Created == "" {
				w.Created = o.Value
			}
		case "resource_legal_date_signature":
			w.Signed = o.Value
		case "work_id_document":
			if strings.HasPrefix(o.Value, "oj:") {
				w.OJRef = strings.TrimPrefix(o.Value, "oj:")
			}
		case "resource_legal_repeals_resource_legal":
			w.Repeals = append(w.Repeals, o.Value)
		case "resource_legal_implicitly_repeals_resource_legal":
			w.ImplicitRepeal = append(w.ImplicitRepeal, o.Value)
		case "resource_legal_based_on_resource_legal":
			w.BasedOnLegal = append(w.BasedOnLegal, o.Value)
		case "resource_legal_based_on_concept_treaty":
			w.Treaties = append(w.Treaties, o.Value)
		}
	}

	// 3) 原始留证：incoming/eu_<celex>_work.json（复检可核对；查询 URL 已进 import_log）
	incoming := filepath.Join(c.DataDir, "incoming")
	if err := os.MkdirAll(incoming, 0o755); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(incoming, "eu_"+celex+"_work.json"), raw, 0o644); err != nil {
		return nil, err
	}
	return w, nil
}

// LawID 由 ELI（优先）或 CELEX 推导稳定标识：eu_reg_2016_679。
func (w *EUWork) LawID() string {
	if u, err := url.Parse(w.ELI); err == nil {
		segs := strings.Split(strings.Trim(u.Path, "/"), "/") // eli/reg/2016/679/oj
		for i, s := range segs {
			if s == "eli" && i+3 < len(segs) {
				return "eu_" + segs[i+1] + "_" + segs[i+2] + "_" + segs[i+3]
			}
		}
	}
	kind := map[string]string{"R": "reg", "L": "dir", "D": "dec", "S": "scd"}[w.TypeLetter]
	if kind == "" {
		kind = "act"
	}
	return fmt.Sprintf("eu_%s_%s_%s", kind, w.Year, w.Number)
}

// EffectLevel CDM 类型字母 → 我们的效力等级（v2 CHECK 允许值）。
func (w *EUWork) EffectLevel() string {
	switch w.TypeLetter {
	case "R":
		return "欧盟条例"
	case "L":
		return "欧盟指令"
	case "D", "S":
		return "欧盟决定"
	default:
		return "其他"
	}
}

// EffectiveDate 生效日（entry-into-force 首个日期）。
func (w *EUWork) EffectiveDate() string {
	if len(w.EntryIntoForce) == 0 {
		return ""
	}
	return w.EntryIntoForce[0]
}

// ApplicationDate 适用日（第二个 entry-into-force；欧盟特有，中国法无此概念）。
func (w *EUWork) ApplicationDate() string {
	if len(w.EntryIntoForce) >= 2 {
		return w.EntryIntoForce[1]
	}
	return ""
}

// ExpireDate 失效日；9999-12-31（CDM 的「永久」占位）按我们的约定转为空串（未失效）。
func (w *EUWork) ExpireDate() string {
	if w.EndOfValidity == "" || w.EndOfValidity >= "9999-01-01" {
		return ""
	}
	return w.EndOfValidity
}

// DefaultName 无人工命名时的保守回退名（标题不在 work graph 中，须人工补充）。
func (w *EUWork) DefaultName() string {
	names := map[string]string{"R": "Regulation (EU)", "L": "Directive (EU)", "D": "Decision (EU)", "S": "Decision (ECSC)"}
	n := names[w.TypeLetter]
	if n == "" {
		n = "EU act"
	}
	return fmt.Sprintf("%s %s/%s", n, w.Year, w.Number)
}

// UpsertLaw 写入/更新 law 主表（幂等；名称为空用保守回退名并备注待人工补充）。
func (c *Cellar) UpsertLaw(w *EUWork, name, shortName, issuer string) (string, error) {
	if name == "" {
		name = w.DefaultName() + " [标题待人工补充]"
	}
	if issuer == "" {
		issuer = "European Union"
	}
	lawID := w.LawID()
	_, err := c.DB.Exec(`INSERT INTO law(law_id,name,short_name,issuer,effect_level,is_local,remark,eli,celex)
		VALUES(?,?,?,?,?,0,?,?,?)
		ON CONFLICT(law_id) DO UPDATE SET eli=excluded.eli, celex=excluded.celex,
			effect_level=excluded.effect_level, issuer=excluded.issuer`,
		lawID, name, nullable(shortName), issuer, w.EffectLevel(),
		nullable("Cellar work graph "+w.GraphURI+" @"+w.FetchedAt+"；关系 pending 待人工确认"),
		nullable(w.ELI), nullable(w.Celex))
	if err != nil {
		return "", fmt.Errorf("写入 law 失败: %w", err)
	}
	return lawID, nil
}

// LinkRelations 把元数据中的废止/依据关系挂到已入库的目标法（FK 要求双方存在）。
// 目标不在库中则跳过并计数——不造假、不自动建占位法；一律 pending 待人工确认。
func (c *Cellar) LinkRelations(ctx context.Context, srcLawID string, w *EUWork) (linked, skipped int, err error) {
	type rel struct {
		targetURI string
		kind      string
	}
	var rels []rel
	for _, u := range w.Repeals {
		rels = append(rels, rel{u, "废止"})
	}
	for _, u := range w.ImplicitRepeal {
		rels = append(rels, rel{u, "废止"})
	}
	for _, u := range w.BasedOnLegal {
		rels = append(rels, rel{u, "依据"})
	}
	for _, r := range rels {
		dst, sErr := c.findLawByCellarURI(ctx, r.targetURI)
		if sErr != nil {
			return linked, skipped, sErr
		}
		if dst == "" {
			skipped++ // 目标法未入库：跳过；人工导入目标法后可重跑补齐
			continue
		}
		if _, err := c.DB.Exec(`INSERT OR IGNORE INTO relation(src_law_id,dst_law_id,relation_type,confirm_status)
			VALUES(?,?,?,'pending')`, srcLawID, dst, r.kind); err != nil {
			return linked, skipped, fmt.Errorf("写入 relation 失败: %w", err)
		}
		linked++
	}
	return linked, skipped, nil
}

// findLawByCellarURI cellar URI → CELEX（sameAs 反查）→ 库内 law_id；未入库返回空串。
func (c *Cellar) findLawByCellarURI(ctx context.Context, cellarURI string) (string, error) {
	rows, err := c.sparql(ctx, fmt.Sprintf(
		"SELECT ?o WHERE { GRAPH ?g { <%s> <http://www.w3.org/2002/07/owl#sameAs> ?o } } LIMIT 20", cellarURI))
	if err != nil {
		return "", err
	}
	for _, b := range rows {
		v := b["o"].Value
		if !strings.HasPrefix(v, cellarResBase+"/celex/") {
			continue
		}
		code := strings.TrimPrefix(v, cellarResBase+"/celex/")
		var lawID string
		qErr := c.DB.QueryRow(`SELECT law_id FROM law WHERE celex = ?`, code).Scan(&lawID)
		if qErr == nil {
			return lawID, nil
		}
		if !errors.Is(qErr, sql.ErrNoRows) {
			return "", fmt.Errorf("查询 law(celex=%s) 失败: %w", code, qErr)
		}
	}
	return "", nil
}

// mustParseHost 取 URL 主机名（入参为包内常量；解析失败回退空串=拒绝一切重定向）。
func mustParseHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (c *Cellar) policy() FetchPolicy {

	pol := c.Policy
	if pol.Timeout <= 0 {
		pol.Timeout = 30 * time.Second
	}
	if pol.UserAgent == "" {
		pol.UserAgent = defaultUserAgent()
	}
	return pol
}

// logQuery SPARQL 访问同样进 import_log（无文件，file_path/sha256 为空）。
func (c *Cellar) logQuery(endpoint, result, reason string) {
	_, _ = c.DB.Exec(`INSERT INTO import_log(doc_id, source_url, file_path, file_size, sha256,
		http_status, content_type, result, reason, created_at)
		VALUES(NULL,?,NULL,NULL,NULL,NULL,'application/sparql-results+json',?,?,?)`,
		endpoint, result, "[cellar] "+reason, time.Now().UTC().Format(time.RFC3339))
}
