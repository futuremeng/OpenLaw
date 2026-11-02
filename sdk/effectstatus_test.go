package sdk

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/futuremeng/OpenLaw/store"
)

// effectDB 在最小语料上补一个可作为关系来源的 law l2（dst=l1 接收关系证据）。
func effectDB(t *testing.T, dir string) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(dir, "db", "openlaw.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO law(law_id, name) VALUES('l2','后法')`); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestEffectStatusTableDriven OL-1 表驱动：五种取值全覆盖；
// 核心验收——relation 为 pending 时绝不出现 REPEALED。
func TestEffectStatusTableDriven(t *testing.T) {
	const today = "2026-06-01"
	cases := []struct {
		name    string
		eff     string
		exp     string
		relType string
		relCS   string // 空 = 无关系
		want    string
	}{
		{"区间覆盖当下无关系", "2020-01-01", "", "", "", EffectEffective},
		{"到期且有confirmed修改=接续", "2020-01-01", "2023-01-01", "修改", "confirmed", EffectAmended},
		{"confirmed废止", "2020-01-01", "2023-01-01", "废止", "confirmed", EffectRepealed},
		{"pending废止绝不REPEALED", "2020-01-01", "", "废止", "pending", EffectRepealedPending},
		{"到期+pending修改待落判", "2020-01-01", "2023-01-01", "修改", "pending", EffectRepealedPending},
		{"到期但关系缺失", "2020-01-01", "2023-01-01", "", "", EffectUnknown},
		{"尚未生效", "2027-01-01", "", "", "", EffectUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := effectDB(t, setupCorpus(t))
			defer db.Close()
			if c.relType != "" {
				if _, err := db.Exec(`INSERT INTO relation(src_law_id,dst_law_id,relation_type,confirm_status)
					VALUES('l2','l1',?,?)`, c.relType, c.relCS); err != nil {
					t.Fatal(err)
				}
			}
			got, err := EffectStatus(db, "l1", c.eff, c.exp, today)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("EffectStatus = %s, 期望 %s", got, c.want)
			}
		})
	}
}

// TestEffectStatusOnOutputs Search/GetArticle/ResolveVersion 三出口必须携带 effect_status。
func TestEffectStatusOnOutputs(t *testing.T) {
	o := openSDK(t)
	if es := o.Search("连带责任", 5).Data["hits"].([]map[string]any)[0]["effect_status"]; es != EffectEffective {
		t.Errorf("Search hit effect_status = %v", es)
	}
	d := o.GetArticle("l1_v1_第一条").Data
	if d["effect_status"] != EffectEffective {
		t.Errorf("GetArticle effect_status = %v", d["effect_status"])
	}
	today := time.Now().Format("2006-01-02")
	rv := o.ResolveVersion("测试法", today)
	if rv.Data["effect_status"] != EffectEffective {
		t.Errorf("ResolveVersion effect_status = %v", rv.Data["effect_status"])
	}
}

// TestPendingRelationNeverRepealed 库中挂 pending 废止关系时，出口必须给
// REPEALED_PENDING，绝不出现 REPEALED（机器不裁决，10 章）。
func TestPendingRelationNeverRepealed(t *testing.T) {
	dir := setupCorpus(t)
	db := effectDB(t, dir)
	if _, err := db.Exec(`INSERT INTO relation(src_law_id,dst_law_id,relation_type,confirm_status)
		VALUES('l2','l1','废止','pending')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	o, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if es := o.GetArticle("l1_v1_第一条").Data["effect_status"]; es != EffectRepealedPending {
		t.Fatalf("pending 废止应 REPEALED_PENDING, got %v", es)
	}
}
