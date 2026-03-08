package commands

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type runServiceConfig struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

type runServiceSupervisor interface {
	Start(ctx context.Context, services []runServiceConfig, dir string) error
	Stop() error
}

type runServiceManager struct {
	logf         func(string)
	logs         *runLogSession
	startProcess runServiceStartFunc
	restartDelay time.Duration
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

type runServiceStartFunc func(ctx context.Context, dir string, service runServiceConfig) (runServiceProcess, error)

type runServiceProcess interface {
	Wait() error
}

type runExecServiceProcess struct {
	cmd     *exec.Cmd
	done    chan struct{}
	closers []io.Closer
}

func newRunServiceManager(logf func(string)) *runServiceManager {
	manager := &runServiceManager{
		logf:         logf,
		restartDelay: 1 * time.Second,
	}
	manager.startProcess = manager.startProcessWithLogs
	return manager
}

func (m *runServiceManager) Start(ctx context.Context, services []runServiceConfig, dir string) error {
	if len(services) == 0 {
		return nil
	}
	if m.startProcess == nil {
		m.startProcess = m.startProcessWithLogs
	}
	if m.restartDelay <= 0 {
		m.restartDelay = time.Second
	}
	if m.logf == nil {
		m.logf = func(string) {}
	}

	serviceCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	for _, service := range services {
		service := service
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.supervise(serviceCtx, dir, service)
		}()
	}
	return nil
}

func (m *runServiceManager) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	return nil
}

func (m *runServiceManager) supervise(ctx context.Context, dir string, service runServiceConfig) {
	startedOnce := false
	for ctx.Err() == nil {
		process, err := m.startProcess(ctx, dir, service)
		if err != nil {
			m.log(fmt.Sprintf("info: service %s failed to start: %v", service.Name, err))
			if !runServiceSleep(ctx, m.restartDelay) {
				return
			}
			continue
		}

		if startedOnce {
			m.log(fmt.Sprintf("info: service %s restarted", service.Name))
		} else {
			m.log(fmt.Sprintf("info: service %s started", service.Name))
			startedOnce = true
		}

		err = process.Wait()
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.log(fmt.Sprintf("info: service %s exited: %v; restarting", service.Name, err))
		} else {
			m.log(fmt.Sprintf("info: service %s exited; restarting", service.Name))
		}
		if !runServiceSleep(ctx, m.restartDelay) {
			return
		}
	}
}

func (m *runServiceManager) log(line string) {
	if m.logf != nil {
		m.logf(line)
	}
	if m.logs != nil {
		m.logs.Logf("%s", line)
	}
}

func runServiceSleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (m *runServiceManager) startProcessWithLogs(ctx context.Context, dir string, service runServiceConfig) (runServiceProcess, error) {
	return startRunServiceProcess(ctx, dir, service, m.logs)
}

func startRunServiceProcess(ctx context.Context, dir string, service runServiceConfig, logs *runLogSession) (runServiceProcess, error) {
	cmd := exec.Command(defaultRunServiceShell(), "-lc", service.Command)
	cmd.Dir = dir
	var closers []io.Closer
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if logs != nil {
		stdoutFile, stderrFile, err := logs.OpenServiceLogs(service.Name)
		if err != nil {
			return nil, fmt.Errorf("open service logs for %s: %w", service.Name, err)
		}
		cmd.Stdout = stdoutFile
		cmd.Stderr = stderrFile
		closers = append(closers, stdoutFile, stderrFile)
	}
	setRunServiceProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		for _, closer := range closers {
			_ = closer.Close()
		}
		return nil, err
	}

	done := make(chan struct{})
	go runServiceWatchContext(ctx, cmd, done)

	return &runExecServiceProcess{cmd: cmd, done: done, closers: closers}, nil
}

func (p *runExecServiceProcess) Wait() error {
	err := p.cmd.Wait()
	close(p.done)
	for _, closer := range p.closers {
		_ = closer.Close()
	}
	return err
}

func runServiceWatchContext(ctx context.Context, cmd *exec.Cmd, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		stopRunServiceCommand(cmd)
	case <-done:
	}
}

func stopRunServiceCommand(cmd *exec.Cmd) {
	killRunServiceProcessGroup(cmd)
}

func defaultRunServiceShell() string {
	return "/bin/sh"
}

func formatRunServicesPromptSection(services []runServiceConfig) string {
	lines := make([]string, 0, len(services)+1)
	for _, service := range services {
		name := strings.TrimSpace(service.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(service.Description)
		if description == "" {
			description = strings.TrimSpace(service.Command)
		}
		if description == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", name, description))
	}
	if len(lines) == 0 {
		return ""
	}
	return "Services available:\n" + strings.Join(lines, "\n")
}
