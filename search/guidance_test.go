// guidance_test.go 检索的诚实出路：零命中必须给出原因与可照做的下一步；
// 别名指向必须是库内事实；达上限必须说明还有更多。检索本身不重排序的边界不变。
package search

import (
	"strings"
	"testing"
)

func TestSearchGuidedZeroResult(t *testing.T) {
	db := setupDB(t)
	hits, g, err := SearchGuided(db, "海商法承运人责任", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 || !g.ZeroResult {
		t.Fatalf("应零命中并标注，got %d %+v", len(hits), g)
	}
	if len(g.Reasons) == 0 || len(g.Suggestions) == 0 {
		t.Fatalf("零命中必须给出原因与下一步: %+v", g)
	}
	joined := strings.Join(g.Reasons, "|") + strings.Join(g.Suggestions, "|")
	if !strings.Contains(joined, "不做同义改写") {
		t.Errorf("必须如实说明不做语义改写: %s", joined)
	}
}

func TestSearchGuidedAliasTargets(t *testing.T) {
	db := setupDB(t)
	if _, err := db.Exec(`INSERT INTO law_alias(alias,law_id,source,built_at)
		VALUES('测试条例','l1','corpus-text','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, g, err := SearchGuided(db, "测试条例", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.AliasOf) != 1 || g.AliasOf[0] != "测试法" {
		t.Errorf("别名应指向库内法规全称: %+v", g.AliasOf)
	}
	found := false
	for _, s := range g.Suggestions {
		if strings.Contains(s, "测试法") {
			found = true
		}
	}
	if !found {
		t.Errorf("别名命中必须进入下一步建议: %+v", g.Suggestions)
	}
}

func TestSearchGuidedCapped(t *testing.T) {
	db := setupDB(t)
	if _, err := db.Exec(`INSERT INTO article(article_id,version_id,article_no,content,md_path,doc_id,parser_version)
		VALUES('l1_v1_第二条','l1_v1','第二条','股东滥用有限责任的，承担连带责任。','md/d1.md','d1','test')`); err != nil {
		t.Fatal(err)
	}
	hits, g, err := SearchGuided(db, "连带责任", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !g.Capped {
		t.Fatalf("命中数达上限必须标注 Capped: %d %+v", len(hits), g)
	}
	if g.ZeroResult {
		t.Error("有命中不得标注零命中")
	}
	if !strings.Contains(strings.Join(g.Suggestions, "|"), "库里可能还有更多") {
		t.Errorf("达上限必须说明还有更多: %+v", g.Suggestions)
	}
}

func TestSearchGuidedNoAliasLayer(t *testing.T) {
	db := setupDB(t)
	if _, err := db.Exec(`DROP TABLE law_alias`); err != nil {
		t.Fatal(err)
	}
	_, g, err := SearchGuided(db, "某个简称", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.AliasOf) != 0 {
		t.Errorf("无别名表时不得凭空给出指向: %+v", g.AliasOf)
	}
	if !strings.Contains(strings.Join(g.Suggestions, "|"), "尚未构建法名别名表") {
		t.Errorf("缺别名层必须如实说明并给指引: %+v", g.Suggestions)
	}
}
