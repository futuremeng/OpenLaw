# 设计：双智能体自审核与例外纠正（agent review）

状态：已落地（2026-10-30）。代码落点：`agent/`（流水线）、`desktop/agentreview.go`
（桌面接线）、`ingest/confirm.go`（落判 runner + `confirm_source`）、`store/`（schema v5）。
界面契约见 docs/design-ui-language.md 与 `desktop/ui_contract_test.go`。

## 目标

把「每条都要人工复核」改为「双智能体自审核 → 机器只裁决有把握的 → 人工只处理例外」。
人工参与降到最低（例外队列一键纠正），但审计纪律一寸不让：每一次结论都署名、
写明依据、进 review_log，每一次审核都留有原始结论可查。

## 分工与流程

1. **分类智能体（classifier）**：读条目元数据与原文首尾摘要，产出效力等级、
   发布机关、公布日期、地域区划码的结构化建议与置信度（0–1）。
2. **审核智能体（auditor）**：比对官方原文与元数据及分类建议，裁决
   `confirm`（可自动确认）或 `escalate`（转人工）。
3. **门槛落判**：两个置信度都 ≥ `DefaultMinConfidence`（0.85）才自动确认；
   任一不达标即降级为 escalate，并把降级原因写进留痕。
4. **例外队列**：escalate 的条目保持 `pending`，进入「智能审核」面板的
   「需要您纠正的条目」列表；人工在右列详情一键确认/驳回（`CorrectDoc`，
   署名固定「本机用户（人工纠正）」，结论依据必填）。

## fail-closed 纪律（不可绕过）

- 默认离线：智能体通道必须由用户显式配置环境变量（`OPENLAW_LLM_ENDPOINT` /
  `OPENLAW_LLM_KEY` / `OPENLAW_LLM_MODEL`）并显式触发；未配置一律拒绝任务，
  界面给出白话指引（`agent.Guidance`），变量名只出现在「技术细节」折叠。
- 任何调用失败、输出不合格式、置信度不足，一律 escalate——宁可转人工，绝不误确认。
- 中断（context 取消）即停，已审结果与留痕如实保留（`Summary.StoppedAt`）。
- 任务经单槽 TaskManager（`kind=agent-review`，互斥、可取消、进度白话回显）。

## 数据模型（schema v5）

- `document.confirm_source`：`agent`=智能体确认，`human`=人工确认，
  空=历史数据（来源未记录）。reset（退回重审）清空该字段。
- `agent_review` 留痕表：每次审核一行——`doc_id / verdict / confidence /
  classify（分类原始 JSON）/ audit（审核有效结论）/ model / created_at`。
  留痕表只增不改，是例外队列「为什么需要您纠正」与事后抽查的依据。

## 诚实展示（最高优先）

- 统计与徽章必须三口径分列：**智能体确认 / 人工确认 / 历史确认（来源未记录）**。
  历史确认不得冒充智能体确认或人工确认（徽章沿用中性「已复核」，不带来源）。
- 例外条目必须如实给出没把握的原因（从留痕 JSON 提取；提不出就明说
  「智能体未给出可读依据」），并附分类建议与置信度。
- 「尚未审核」（无留痕）与「需要您纠正」（已审但 escalate）分开计数，不得混同。

## 同一代码路径

- 自动确认与人工纠正都经 `ingest.Confirmer.Judge`（与 CLI openlaw-confirm 同一
  runner），只是 `confirm_source` 与署名不同；前端不直连数据库。
- 统计口径（AgentStats）直接查库的 status+source 组合，不另设计数器。

## 界面接线（desktop）

- `AgentStatus`：通道状态（configured / model / guidance / technical）。
  未配置时「开始智能审核」按钮禁用并显示指引。
- `StartReviewTask(limit)`：提交一轮审核任务；前端 `rvTick` 轮询 TaskList 回显进度。
- `AgentStats` / `ExceptionList` / `CorrectDoc`：统计、例外队列、一键纠正。
- 面板结构（三栏右列详情）：统计卡 → 通道状态 → 开始审核 → 例外队列 →
  右列详情 + 底部纠正工具条（结论依据必填 + 确认/驳回）。

## 验证

- `agent/review_test.go`：桩 Chat 离线跑通——门槛降级、解析失败转人工、
  留痕落表、中断保留。
- `desktop/binding_test.go` + `wiring_smoke.mjs` + `ui_contract_test.go`：
  绑定契约与界面契约（含「旧人工复核控件不得回流」门禁）。
