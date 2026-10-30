// guidance_test.go：界面语言纪律——给用户的指引只说可照做的步骤，端点形态、参数名与
// 环境变量名一律留在 GuidanceTech（界面只放「技术细节」折叠）。见 docs/design-ui-language.md。
package browser

import (
	"strings"
	"testing"
)

func TestGuidancePlainLanguage(t *testing.T) {
	for _, banned := range []string{"http://", "127.0.0.1", "-endpoint", "OPENLAW_BROWSEROS_ENDPOINT", "URL"} {
		if strings.Contains(Guidance, banned) {
			t.Errorf("Guidance 不得含技术细节 %q（应只出现在 GuidanceTech）：%s", banned, Guidance)
		}
	}
	for _, need := range []string{"-endpoint", "OPENLAW_BROWSEROS_ENDPOINT", "127.0.0.1"} {
		if !strings.Contains(GuidanceTech, need) {
			t.Errorf("GuidanceTech 应保留技术写法 %q 供「技术细节」显示：%s", need, GuidanceTech)
		}
	}
	if !strings.Contains(Guidance, "不会改用其他获取方式") {
		t.Errorf("Guidance 应明确不得降级获取：%s", Guidance)
	}
}
