package element

import (
	"testing"
)

// TestListConfirmedElementsOnlyConfirmed OL-3：只有已复核确认的要件才是封闭候选；
// 案由未登记与「有案由但全部待复核」必须区分（not_found vs ok+空表）。
func TestListConfirmedElementsOnlyConfirmed(t *testing.T) {
	db := setupDB(t)
	if _, err := Import(db, []Input{
		in1(),
		{ElementID: "e2", ArticleID: "a1", CaseType: "民间借贷", Name: "款项交付"},
		{ElementID: "e3", ArticleID: "a1", CaseType: "买卖合同", Name: "标的物交付"},
	}, true); err != nil {
		t.Fatal(err)
	}
	known, err := CaseTypeKnown(db, "民间借贷")
	if err != nil || !known {
		t.Fatalf("案由应已登记: %v %v", known, err)
	}
	if known, err := CaseTypeKnown(db, "不存在的案由"); err != nil || known {
		t.Fatalf("未登记案由必须报未找到: %v %v", known, err)
	}

	// 全部 pending：不得作为候选
	items, err := ListConfirmedElements(db, "民间借贷")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("pending 要件不得进入候选: %+v", items)
	}
	if ok, err := ConfirmStatusKnown(db); err != nil || !ok {
		t.Errorf("v9 语料应记录确认状态: %v %v", ok, err)
	}

	// 人工落判后进入候选，且只列该案下的
	if _, err := Confirm(db, []string{"e1", "e3"}, "confirm", "王复核", "对照原文"); err != nil {
		t.Fatal(err)
	}
	items, err = ListConfirmedElements(db, "民间借贷")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ElementID != "e1" || items[0].Name != "借贷合意" {
		t.Fatalf("应只列已确认的本案由要件: %+v", items)
	}
}

// TestListElementsOldCorpusHonest 旧语料（无 confirm_status 列）：一律按待复核处理，
// 不得把未确认要件当作已确认候选（诚实展示纪律）。
func TestListElementsOldCorpusHonest(t *testing.T) {
	db := setupDB(t)
	if _, err := db.Exec(`ALTER TABLE element DROP COLUMN confirm_status`); err != nil {
		t.Skipf("当前 SQLite 不支持 DROP COLUMN，跳过： %v", err)
	}
	if _, err := db.Exec(`INSERT INTO element(element_id, article_id, case_type, name)
		VALUES('e9','a1','民间借贷','借贷合意')`); err != nil {
		t.Fatal(err)
	}
	if ok, err := ConfirmStatusKnown(db); err != nil || ok {
		t.Fatalf("旧语料应报未记录确认状态: %v %v", ok, err)
	}
	items, err := ListConfirmedElements(db, "民间借贷")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("旧语料不得给出已确认要件: %+v", items)
	}
}
