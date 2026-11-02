package sdk

import (
	"github.com/futuremeng/OpenLaw/store"
)

// effect_status 显式效力状态（OL-1，docs/tasks-opencourt-bridge.md）。
// 判定发生在语料层之内、上层之外：只依据 law_version 版本链 + relation 关系表，
// 且**仅 confirm_status=confirmed 的关系可参与裁决**（10 章纪律：机器不自动判定
// 废止）。不确定一律 UNKNOWN，不猜。上层（OpenCourt）据此停止自行推导效力。
//
// | 值 | 判定依据 |
// |---|---|
// | EFFECTIVE | 版本区间覆盖当下，且无废止/接续关系证据 |
// | AMENDED | 被后续版本接续（expire_date 已到期）且有 confirmed 的修改关系 |
// | REPEALED | 存在 confirmed 的废止关系（relation 为 pending 时绝不出现此值） |
// | REPEALED_PENDING | 存在废止/接续关系但 confirm_status != confirmed |
// | UNKNOWN | 版本链不完整或关系缺失（如 expire_date 已置却无任何关系佐证） |
const (
	EffectEffective       = "EFFECTIVE"
	EffectAmended         = "AMENDED"
	EffectRepealed        = "REPEALED"
	EffectRepealedPending = "REPEALED_PENDING"
	EffectUnknown         = "UNKNOWN"
)

// EffectStatus 计算某法规版本在 today（YYYY-MM-DD）的效力状态。
// 输入是已解析出的版本行（effective/expire 日期）与其所属 law_id；
// 关系证据取 dst_law_id 收到的「废止」「修改」两类（接续=修改关系）。
// 判定顺序（confirmed 证据优先，pending 只降级不裁决）：
//  1. confirmed 废止 → REPEALED；
//  2. 未确认废止 → REPEALED_PENDING（无论版本区间如何——待人工落判）；
//  3. 版本已到期（expire 非空且 ≤ today）：confirmed 修改 → AMENDED；
//     未确认修改 → REPEALED_PENDING；到期却无任何关系佐证 → UNKNOWN（关系缺失）；
//  4. 版本尚未生效（effective > today）→ UNKNOWN（版本链不完整）；
//  5. 其余（区间覆盖当下、无废止/接续证据）→ EFFECTIVE。
func EffectStatus(db *store.DB, lawID, effectiveDate, expireDate, today string) (string, error) {
	var confirmedRepeal, pendingRepeal, confirmedAmend, pendingAmend bool
	rows, err := db.Query(`SELECT relation_type, confirm_status FROM relation
		WHERE dst_law_id = ? AND relation_type IN ('废止','修改')`, lawID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var typ, cs string
		if err := rows.Scan(&typ, &cs); err != nil {
			return "", err
		}
		switch {
		case typ == "废止" && cs == "confirmed":
			confirmedRepeal = true
		case typ == "废止":
			pendingRepeal = true
		case typ == "修改" && cs == "confirmed":
			confirmedAmend = true
		default:
			pendingAmend = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch {
	case confirmedRepeal:
		return EffectRepealed, nil
	case pendingRepeal:
		return EffectRepealedPending, nil
	case expireDate != "" && expireDate <= today:
		if confirmedAmend {
			return EffectAmended, nil
		}
		if pendingAmend {
			return EffectRepealedPending, nil
		}
		return EffectUnknown, nil // 到期却无关系佐证：链不完整，不猜
	case effectiveDate > today:
		return EffectUnknown, nil // 尚未生效
	default:
		return EffectEffective, nil
	}
}
