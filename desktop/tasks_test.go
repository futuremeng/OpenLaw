package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func tmEvents() (*TaskManager, *[]TaskInfo) {
	var mu sync.Mutex
	var ev []TaskInfo
	tm := NewTaskManager(func(event string, data ...interface{}) {
		if event == "task" {
			mu.Lock()
			ev = append(ev, data[0].(TaskInfo))
			mu.Unlock()
		}
	})
	return tm, &ev
}

func waitState(t *testing.T, tm *TaskManager, state string) TaskInfo {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ts := tm.Tasks()
		if len(ts) > 0 && ts[0].State == state {
			return ts[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待状态 %s 超时: %+v", state, tm.Tasks())
	return TaskInfo{}
}

func TestTaskManagerExclusiveProgressDone(t *testing.T) {
	tm, ev := tmEvents()
	started := make(chan struct{})
	release := make(chan struct{})
	if _, err := tm.Submit("govcn-import", "p1", func(ctx context.Context, progress func(string)) error {
		close(started)
		progress("1/2 ok x")
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := tm.Submit("govlist-browse", "p2", func(context.Context, func(string)) error { return nil }); err == nil {
		t.Fatal("单槽互斥：运行中第二次提交必须被拒")
	}
	cur := tm.Tasks()[0]
	if cur.State != "running" || cur.Progress != "1/2 ok x" {
		t.Fatalf("运行态快照不对: %+v", cur)
	}
	close(release)
	done := waitState(t, tm, "done")
	if done.Params != "p1" || done.EndedAt == "" {
		t.Fatalf("完成态不对: %+v", done)
	}
	if len(tm.Tasks()) != 1 {
		t.Fatalf("完成后当前清空、历史留 1: %+v", tm.Tasks())
	}
	if len(*ev) < 3 {
		t.Fatalf("事件应至少 启动/进度/完成 三条: %d", len(*ev))
	}
}

func TestTaskManagerCancel(t *testing.T) {
	tm, _ := tmEvents()
	ran := make(chan struct{})
	if _, err := tm.Submit("govlist-browse", "", func(ctx context.Context, progress func(string)) error {
		close(ran)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	<-ran
	if !tm.Cancel() {
		t.Fatal("Cancel 应报告有任务被取消")
	}
	if info := waitState(t, tm, "cancelled"); info.Detail != "" {
		t.Fatalf("取消不应记 failed 详情: %+v", info)
	}
	if tm.Cancel() {
		t.Fatal("无任务时 Cancel 应返回 false")
	}
}

func TestTaskManagerFailureKeepsDetail(t *testing.T) {
	tm, _ := tmEvents()
	if _, err := tm.Submit("govcn-import", "", func(context.Context, func(string)) error {
		return errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}
	if info := waitState(t, tm, "failed"); !strings.Contains(info.Detail, "boom") {
		t.Fatalf("失败详情丢失: %+v", info)
	}
}

func TestTaskHistoryRing(t *testing.T) {
	tm, _ := tmEvents()
	for i := 0; i < 25; i++ {
		if _, err := tm.Submit("govcn-import", "", func(context.Context, func(string)) error { return nil }); err != nil {
			t.Fatal(err)
		}
		waitState(t, tm, "done")
	}
	if ts := tm.Tasks(); len(ts) != 20 || ts[0].ID != 25 {
		t.Fatalf("环形历史应为最近 20 条且最新在前: len=%d first=%+v", len(ts), ts[0])
	}
}
