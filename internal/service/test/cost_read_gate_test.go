package test

import (
	"context"
	"errors"
	"testing"
	"time"

	"cpa-usage-keeper/internal/service"
)

func TestCostReadGateBlocksNewReadsAndDrainsExistingLease(t *testing.T) {
	gate := service.NewCostReadGate()
	release, ok := gate.Acquire()
	if !ok {
		t.Fatal("initial fee read was rejected")
	}
	type drainResult struct {
		resume func()
		err    error
	}
	drained := make(chan drainResult, 1)
	go func() {
		resume, err := gate.BlockAndDrain(context.Background())
		drained <- drainResult{resume: resume, err: err}
	}()
	waitForCostReadGateBlocked(t, gate)
	select {
	case result := <-drained:
		t.Fatalf("drain returned with active read: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	release() // 重复释放不能将计数减成负数。
	var result drainResult
	select {
	case result = <-drained:
	case <-time.After(time.Second):
		t.Fatal("drain did not finish after lease release")
	}
	if result.err != nil || result.resume == nil {
		t.Fatalf("drain returned %+v", result)
	}
	if _, ok := gate.Acquire(); ok {
		t.Fatal("gate reopened before coordinator resumed reads")
	}
	result.resume()
	result.resume()
	if releaseAfter, ok := gate.Acquire(); !ok {
		t.Fatal("gate did not reopen")
	} else {
		releaseAfter()
	}
}

func TestCostReadGateCanceledDrainRestoresReads(t *testing.T) {
	gate := service.NewCostReadGate()
	release, ok := gate.Acquire()
	if !ok {
		t.Fatal("initial fee read was rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	type drainResult struct {
		resume func()
		err    error
	}
	drained := make(chan drainResult, 1)
	go func() {
		resume, err := gate.BlockAndDrain(ctx)
		drained <- drainResult{resume: resume, err: err}
	}()
	waitForCostReadGateBlocked(t, gate)
	cancel()
	select {
	case result := <-drained:
		if !errors.Is(result.err, context.Canceled) || result.resume != nil {
			t.Fatalf("canceled drain returned %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled drain did not return")
	}
	if releaseAfter, ok := gate.Acquire(); !ok {
		t.Fatal("cancel left fee reads blocked")
	} else {
		releaseAfter()
	}
	release()
}

func TestCostReadGateRejectsSecondBlockWithoutQueueing(t *testing.T) {
	gate := service.NewCostReadGate()
	resume, err := gate.BlockAndDrain(context.Background())
	if err != nil || resume == nil {
		t.Fatalf("first block failed: %v", err)
	}
	if another, err := gate.BlockAndDrain(context.Background()); err == nil || another != nil {
		t.Fatalf("second block should fail without a resume function: resume=%v err=%v", another != nil, err)
	}
	resume()
	if release, ok := gate.Acquire(); !ok {
		t.Fatal("first block was not resumed")
	} else {
		release()
	}
}

func waitForCostReadGateBlocked(t *testing.T, gate *service.CostReadGate) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		release, ok := gate.Acquire()
		if !ok {
			return
		}
		// 轮询探测成功时必须释放刚获得的许可。
		release()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("gate did not block new reads")
}
