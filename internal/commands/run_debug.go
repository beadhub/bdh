package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type runLogSession struct {
	dir          string
	runLogPath   string
	debugEnabled bool

	mu     sync.Mutex
	runLog *os.File
}

func newRunLogSession(baseDir string, debugEnabled bool, now time.Time) (*runLogSession, error) {
	prefix := fmt.Sprintf("bdh-run-%s-", now.UTC().Format("20060102-150405"))
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, fmt.Errorf("create run log dir: %w", err)
	}

	runLogPath := filepath.Join(dir, "run.log")
	runLog, err := os.OpenFile(runLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open run log: %w", err)
	}

	session := &runLogSession{
		dir:          dir,
		runLogPath:   runLogPath,
		debugEnabled: debugEnabled,
		runLog:       runLog,
	}
	session.Logf("session started at %s", now.UTC().Format(time.RFC3339))
	if strings.TrimSpace(baseDir) != "" {
		session.Logf("working_dir=%s", baseDir)
	}
	return session, nil
}

func (s *runLogSession) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runLog == nil {
		return nil
	}
	err := s.runLog.Close()
	s.runLog = nil
	return err
}

func (s *runLogSession) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

func (s *runLogSession) RunLogPath() string {
	if s == nil {
		return ""
	}
	return s.runLogPath
}

func (s *runLogSession) DebugEnabled() bool {
	if s == nil {
		return false
	}
	return s.debugEnabled
}

func (s *runLogSession) Logf(format string, args ...any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runLog == nil {
		return
	}
	_, _ = fmt.Fprintf(s.runLog, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (s *runLogSession) OpenProviderStderr() (io.WriteCloser, error) {
	return s.openFile("provider.stderr.log")
}

func (s *runLogSession) OpenServiceLogs(name string) (stdout io.WriteCloser, stderr io.WriteCloser, err error) {
	safeName := sanitizeRunLogComponent(name)
	stdout, err = s.openFile(fmt.Sprintf("service-%s.stdout.log", safeName))
	if err != nil {
		return nil, nil, err
	}
	stderr, err = s.openFile(fmt.Sprintf("service-%s.stderr.log", safeName))
	if err != nil {
		_ = stdout.Close()
		return nil, nil, err
	}
	return stdout, stderr, nil
}

func (s *runLogSession) openFile(name string) (*os.File, error) {
	if s == nil {
		return nil, nil
	}
	path := filepath.Join(s.dir, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return file, nil
}

var runLogComponentPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeRunLogComponent(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	value = runLogComponentPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "unnamed"
	}
	return value
}
