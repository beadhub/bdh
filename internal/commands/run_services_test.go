package commands

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRunServiceProcess struct {
	wait func() error
}

func (p fakeRunServiceProcess) Wait() error {
	return p.wait()
}

func TestRunServiceManagerRestartsExitedService(t *testing.T) {
	var mu sync.Mutex
	starts := 0
	logs := []string{}
	manager := &runServiceManager{
		logf: func(line string) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, line)
		},
		restartDelay: time.Millisecond,
		startProcess: func(ctx context.Context, dir string, service runServiceConfig) (runServiceProcess, error) {
			mu.Lock()
			starts++
			current := starts
			mu.Unlock()
			return fakeRunServiceProcess{wait: func() error {
				if current == 1 {
					return errors.New("exit status 1")
				}
				<-ctx.Done()
				return ctx.Err()
			}}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Start(ctx, []runServiceConfig{{Name: "backend", Command: "make run-backend"}}, ""); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		if starts >= 2 {
			mu.Unlock()
			break
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
	}

	cancel()
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if starts < 2 {
		t.Fatalf("expected restart after exit, got %d starts", starts)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "info: service backend started") {
		t.Fatalf("expected start log, got %q", joined)
	}
	if !strings.Contains(joined, "info: service backend exited: exit status 1; restarting") {
		t.Fatalf("expected restart log, got %q", joined)
	}
	if !strings.Contains(joined, "info: service backend restarted") {
		t.Fatalf("expected restarted log, got %q", joined)
	}
}

func TestRunLoopStartsServicesBeforeFirstRun(t *testing.T) {
	serviceStarted := false
	runStarted := false
	var runnerOrderErr error
	supervisor := &fakeRunServiceSupervisor{
		start: func(_ context.Context, services []runServiceConfig, dir string) error {
			serviceStarted = len(services) == 1 && services[0].Name == "backend" && dir == "/tmp/work"
			return nil
		},
	}

	loop := &runLoop{
		provider:          claudeProvider{},
		now:               time.Now,
		out:               ioDiscard{},
		sleep:             func(context.Context, time.Duration) error { return nil },
		serviceSupervisor: supervisor,
		runner: func(_ context.Context, _ string, _ []string, onLine func(string)) error {
			if !serviceStarted {
				runnerOrderErr = errors.New("expected services to start before first run")
			}
			runStarted = true
			onLine(`{"type":"result","duration_ms":1000}`)
			return nil
		},
	}

	err := loop.Run(context.Background(), runLoopOptions{
		Prompt:      "inspect workspace",
		MaxRuns:     1,
		WorkingDir:  "/tmp/work",
		Services:    []runServiceConfig{{Name: "backend", Command: "make run-backend", Description: "Backend API"}},
		WaitSeconds: 0,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if runnerOrderErr != nil {
		t.Fatal(runnerOrderErr)
	}
	if !runStarted {
		t.Fatal("expected run to start")
	}
	if !supervisor.stopped {
		t.Fatal("expected services to stop when run loop exits")
	}
}

type fakeRunServiceSupervisor struct {
	start   func(ctx context.Context, services []runServiceConfig, dir string) error
	stopped bool
}

func (f *fakeRunServiceSupervisor) Start(ctx context.Context, services []runServiceConfig, dir string) error {
	if f.start != nil {
		return f.start(ctx, services, dir)
	}
	return nil
}

func (f *fakeRunServiceSupervisor) Stop() error {
	f.stopped = true
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
