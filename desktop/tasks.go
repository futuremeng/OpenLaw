// tasks.go：桌面端采集任务管理器。纪律：
//   - 单槽互斥：同一时刻只允许一个采集任务（礼貌限速 + 防并发写库/写报告）；
//   - 可取消：context 取消贯穿到 ingest/browser 层的限速点；
//   - 如实留痕：任务状态机 running/done/failed/cancelled，历史环形留 20 条，
//     每次状态变化经 Wails 事件 "task" 推给前端；
//   - 任务体只调用共享 runner（ingest.RunGovcnBatch / browser.RunGovListAgent），
//     与 CLI 同一代码路径——桌面端不是第二写路径。
package main

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

// TaskInfo 任务快照（绑定给前端）。
type TaskInfo struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"`
	Params    string `json:"params"`
	State     string `json:"state"` // running | done | failed | cancelled
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Progress  string `json:"progress,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// TaskManager 单槽任务管理器。
type TaskManager struct {
	mu      sync.Mutex
	cur     *TaskInfo
	cancel  context.CancelFunc
	history []TaskInfo // 最近在前
	nextID  int
	emit    func(event string, payload ...interface{})
	// onDone 任务终态回调（本机台账用；只给种类与状态，不给参数内容）。
	onDone func(kind, state string)
}

func NewTaskManager(emit func(event string, payload ...interface{})) *TaskManager {
	return &TaskManager{emit: emit}
}

// Submit 启动任务；已有任务在跑则拒绝（错误里带当前任务，便于 UI 提示）。
func (m *TaskManager) Submit(kind, params string, fn func(ctx context.Context, progress func(string)) error) (TaskInfo, error) {
	m.mu.Lock()
	if m.cur != nil {
		c := *m.cur
		m.mu.Unlock()
		return TaskInfo{}, errors.New("已有任务正在进行（任务 #" + strconv.Itoa(c.ID) + "），可先取消后再开始")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.nextID++
	info := &TaskInfo{ID: m.nextID, Kind: kind, Params: params, State: "running",
		StartedAt: time.Now().UTC().Format(time.RFC3339)}
	m.cur = info
	m.cancel = cancel
	snap := *info
	m.mu.Unlock()
	m.emitTask(snap)
	go m.run(ctx, info, fn)
	return snap, nil
}

func (m *TaskManager) run(ctx context.Context, info *TaskInfo, fn func(context.Context, func(string)) error) {
	progress := func(s string) {
		m.mu.Lock()
		info.Progress = s
		snap := *info
		m.mu.Unlock()
		m.emitTask(snap)
	}
	err := fn(ctx, progress)
	m.mu.Lock()
	switch {
	case err == nil:
		info.State = "done"
	case errors.Is(err, context.Canceled):
		info.State = "cancelled"
	default:
		info.State = "failed"
		info.Detail = err.Error()
	}
	info.EndedAt = time.Now().UTC().Format(time.RFC3339)
	m.cur = nil
	m.cancel = nil
	m.history = append([]TaskInfo{*info}, m.history...)
	if len(m.history) > 20 {
		m.history = m.history[:20]
	}
	snap := *info
	m.mu.Unlock()
	m.emitTask(snap)
	if m.onDone != nil {
		m.onDone(info.Kind, snap.State)
	}
}

// Cancel 请求取消当前任务；返回是否有任务被取消请求。
func (m *TaskManager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return false
	}
	m.cancel()
	return true
}

// Tasks 当前任务（在前）+ 历史（最近在前）。
func (m *TaskManager) Tasks() []TaskInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TaskInfo, 0, len(m.history)+1)
	if m.cur != nil {
		out = append(out, *m.cur)
	}
	return append(out, m.history...)
}

func (m *TaskManager) emitTask(info TaskInfo) {
	if m.emit != nil {
		m.emit("task", info)
	}
}
