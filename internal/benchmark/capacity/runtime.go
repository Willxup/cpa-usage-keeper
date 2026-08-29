package capacity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type UnitState struct {
	ActiveState    string `json:"active_state"`
	SubState       string `json:"sub_state"`
	Result         string `json:"result"`
	ExecMainCode   string `json:"exec_main_code"`
	ExecMainStatus int    `json:"exec_main_status"`
	ControlGroup   string `json:"control_group"`
}

type SystemdRuntime struct {
	UnitName   string
	CgroupPath string
	Limits     CgroupLimits
}

type CgroupLimits struct {
	CPUQuotaUsec       int64  `json:"cpu_quota_usec"`
	CPUPeriodUsec      int64  `json:"cpu_period_usec"`
	AllowedCPUs        string `json:"allowed_cpus"`
	MemoryMaxBytes     int64  `json:"memory_max_bytes"`
	MemoryMaxUnlimited bool   `json:"memory_max_unlimited"`
	MemorySwapMaxBytes int64  `json:"memory_swap_max_bytes"`
}

type KeeperStartOptions struct {
	UnitName     string
	BinaryPath   string
	Environment  string
	WorkingDir   string
	StdoutPath   string
	StderrPath   string
	CPU          int
	MemoryMiB    int
	AllowedCPUs  string
	StartupLimit time.Duration
}

func StartKeeperUnit(ctx context.Context, options KeeperStartOptions) (SystemdRuntime, error) {
	if strings.TrimSpace(options.UnitName) == "" || strings.TrimSpace(options.BinaryPath) == "" || strings.TrimSpace(options.Environment) == "" {
		return SystemdRuntime{}, fmt.Errorf("keeper unit name, binary, and environment are required")
	}
	if options.CPU <= 0 || options.MemoryMiB < 0 || strings.TrimSpace(options.AllowedCPUs) == "" {
		return SystemdRuntime{}, fmt.Errorf("keeper CPU, memory, and affinity are required")
	}
	if options.StartupLimit <= 0 {
		options.StartupLimit = 90 * time.Second
	}
	if err := os.MkdirAll(options.WorkingDir, 0o755); err != nil {
		return SystemdRuntime{}, fmt.Errorf("create keeper working directory: %w", err)
	}
	for _, path := range []string{options.StdoutPath, options.StderrPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return SystemdRuntime{}, fmt.Errorf("create keeper log directory: %w", err)
		}
	}
	arguments := []string{
		"--unit=" + options.UnitName,
		"--service-type=exec",
		"--property=Restart=no",
		"--property=KillMode=mixed",
		"--property=TimeoutStopSec=15s",
		"--property=CPUAccounting=yes",
		"--property=MemoryAccounting=yes",
		"--property=IOAccounting=yes",
		fmt.Sprintf("--property=CPUQuota=%d%%", options.CPU*100),
		"--property=AllowedCPUs=" + options.AllowedCPUs,
	}
	if options.MemoryMiB > 0 {
		arguments = append(arguments, fmt.Sprintf("--property=MemoryMax=%d", int64(options.MemoryMiB)*1024*1024))
	}
	arguments = append(arguments,
		"--property=MemorySwapMax=0",
		"--property=TasksMax=512",
		"--property=OOMPolicy=stop",
		"--property=WorkingDirectory="+options.WorkingDir,
		"--property=StandardOutput=append:"+options.StdoutPath,
		"--property=StandardError=append:"+options.StderrPath,
		fmt.Sprintf("--setenv=GOMAXPROCS=%d", options.CPU),
		"--setenv=GOTRACEBACK=all",
		options.BinaryPath,
		"-env", options.Environment,
		"-host", "127.0.0.1",
	)
	if output, err := exec.CommandContext(ctx, "systemd-run", arguments...).CombinedOutput(); err != nil {
		return SystemdRuntime{}, fmt.Errorf("start constrained Keeper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	state, err := ReadUnitState(ctx, options.UnitName)
	if err != nil {
		_ = StopUnit(context.Background(), options.UnitName)
		return SystemdRuntime{}, err
	}
	if state.ControlGroup == "" {
		_ = StopUnit(context.Background(), options.UnitName)
		return SystemdRuntime{}, fmt.Errorf("Keeper unit %s has no control group", options.UnitName)
	}
	cgroupPath := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(state.ControlGroup, "/"))
	limits, err := ReadCgroupLimits(cgroupPath)
	if err != nil {
		_ = StopUnit(context.Background(), options.UnitName)
		return SystemdRuntime{}, err
	}
	if err := ValidateCgroupLimits(limits, options.CPU, options.MemoryMiB, options.AllowedCPUs); err != nil {
		_ = StopUnit(context.Background(), options.UnitName)
		return SystemdRuntime{}, err
	}
	return SystemdRuntime{UnitName: options.UnitName, CgroupPath: cgroupPath, Limits: limits}, nil
}

type RedisStartOptions struct {
	UnitName    string
	Root        string
	Port        int
	Password    string
	BinaryPath  string
	AllowedCPUs string
}

func StartRedisUnit(ctx context.Context, options RedisStartOptions) (SystemdRuntime, error) {
	if options.Port <= 0 || strings.TrimSpace(options.Password) == "" || strings.TrimSpace(options.Root) == "" || strings.TrimSpace(options.AllowedCPUs) == "" {
		return SystemdRuntime{}, fmt.Errorf("Redis root, port, password, and CPU affinity are required")
	}
	if options.BinaryPath == "" {
		options.BinaryPath = "redis-server"
	}
	redisDir := filepath.Join(options.Root, "redis")
	if err := os.MkdirAll(redisDir, 0o700); err != nil {
		return SystemdRuntime{}, fmt.Errorf("create Redis directory: %w", err)
	}
	stdoutPath := filepath.Join(redisDir, "stdout.log")
	stderrPath := filepath.Join(redisDir, "stderr.log")
	arguments := []string{
		"--unit=" + options.UnitName,
		"--service-type=exec",
		"--property=Restart=no",
		"--property=KillMode=mixed",
		"--property=TimeoutStopSec=10s",
		"--property=AllowedCPUs=" + options.AllowedCPUs,
		"--property=WorkingDirectory=" + redisDir,
		"--property=StandardOutput=append:" + stdoutPath,
		"--property=StandardError=append:" + stderrPath,
		options.BinaryPath,
		"--port", strconv.Itoa(options.Port),
		"--bind", "127.0.0.1",
		"--protected-mode", "yes",
		"--requirepass", options.Password,
		"--appendonly", "no",
		"--save", "",
		"--dir", redisDir,
	}
	if output, err := exec.CommandContext(ctx, "systemd-run", arguments...).CombinedOutput(); err != nil {
		return SystemdRuntime{}, fmt.Errorf("start benchmark Redis: %w: %s", err, strings.TrimSpace(string(output)))
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if err := redisPing(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(options.Port)), options.Password); err == nil {
			state, stateErr := ReadUnitState(ctx, options.UnitName)
			if stateErr != nil {
				_ = StopUnit(context.Background(), options.UnitName)
				return SystemdRuntime{}, stateErr
			}
			cgroupPath := filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(state.ControlGroup, "/"))
			allowedCPUs, allowedErr := ReadCgroupAllowedCPUs(cgroupPath)
			if allowedErr != nil {
				_ = StopUnit(context.Background(), options.UnitName)
				return SystemdRuntime{}, allowedErr
			}
			if allowedCPUs != options.AllowedCPUs {
				_ = StopUnit(context.Background(), options.UnitName)
				return SystemdRuntime{}, fmt.Errorf("Redis effective AllowedCPUs=%q, want %q", allowedCPUs, options.AllowedCPUs)
			}
			return SystemdRuntime{UnitName: options.UnitName, CgroupPath: cgroupPath, Limits: CgroupLimits{AllowedCPUs: allowedCPUs}}, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	state, _ := ReadUnitState(ctx, options.UnitName)
	_ = StopUnit(context.Background(), options.UnitName)
	return SystemdRuntime{}, fmt.Errorf("benchmark Redis did not become ready: %+v", state)
}

func redisPing(ctx context.Context, address, password string) error {
	publisher, err := newRedisPublisher(ctx, address, password)
	if err != nil {
		return err
	}
	defer publisher.Close()
	if err := publisher.writeCommand("PING"); err != nil {
		return err
	}
	if err := publisher.writer.Flush(); err != nil {
		return err
	}
	return publisher.readReply()
}

func ReadUnitState(ctx context.Context, unitName string) (UnitState, error) {
	output, err := exec.CommandContext(ctx, "systemctl", "show", "--no-page", unitName,
		"--property=ActiveState", "--property=SubState", "--property=Result", "--property=ExecMainCode",
		"--property=ExecMainStatus", "--property=ControlGroup").CombinedOutput()
	if err != nil {
		return UnitState{}, fmt.Errorf("inspect unit %s: %w: %s", unitName, err, strings.TrimSpace(string(output)))
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok {
			values[key] = value
		}
	}
	status, _ := strconv.Atoi(values["ExecMainStatus"])
	return UnitState{
		ActiveState: values["ActiveState"], SubState: values["SubState"], Result: values["Result"],
		ExecMainCode: values["ExecMainCode"], ExecMainStatus: status, ControlGroup: values["ControlGroup"],
	}, nil
}

func StopUnit(ctx context.Context, unitName string) error {
	if strings.TrimSpace(unitName) == "" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "systemctl", "stop", unitName).CombinedOutput()
	if err != nil && !strings.Contains(string(output), "not loaded") {
		return fmt.Errorf("stop unit %s: %w: %s", unitName, err, strings.TrimSpace(string(output)))
	}
	_, _ = exec.CommandContext(ctx, "systemctl", "reset-failed", unitName).CombinedOutput()
	return nil
}

func WaitHTTPHealth(ctx context.Context, applicationURL, unitName string, timeout time.Duration) (time.Duration, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	started := time.Now()
	deadline := started.Add(timeout)
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(applicationURL, "/")+"/healthz", nil)
		if err == nil {
			response, requestErr := client.Do(request)
			if requestErr == nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return time.Since(started), nil
				}
			}
		}
		state, stateErr := ReadUnitState(ctx, unitName)
		if stateErr == nil && state.ActiveState != "active" && state.ActiveState != "activating" {
			return time.Since(started), fmt.Errorf("Keeper exited before health check: %+v", state)
		}
		select {
		case <-ctx.Done():
			return time.Since(started), ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return time.Since(started), fmt.Errorf("Keeper health check exceeded %s", timeout)
}

func BackupSQLite(ctx context.Context, sourcePath, destinationPath string) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("SQLite backup source and destination are required")
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create SQLite backup directory: %w", err)
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("SQLite backup destination already exists: %s", filepath.Clean(destinationPath))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect SQLite backup destination: %w", err)
	}
	dotCommand := ".backup '" + strings.ReplaceAll(destinationPath, "'", "''") + "'"
	output, err := exec.CommandContext(ctx, "sqlite3", sourcePath, dotCommand).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clone benchmark SQLite database: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func copyStaticDataset(sourcePath, destinationPath string) (returnErr error) {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return fmt.Errorf("create static dataset clone directory: %w", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open static benchmark dataset: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, source.Close())
	}()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create static dataset clone: %w", err)
	}
	defer func() {
		if closeErr := destination.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, closeErr)
		}
		if returnErr != nil {
			_ = os.Remove(destinationPath)
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("copy static benchmark dataset: %w", err)
	}
	if err := destination.Sync(); err != nil {
		return fmt.Errorf("sync static benchmark dataset: %w", err)
	}
	return nil
}

func RestoreDataset(ctx context.Context, sourcePath, destinationPath string) error {
	if strings.HasSuffix(sourcePath, ".zst") {
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return fmt.Errorf("create compressed dataset restore directory: %w", err)
		}
		destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("create restored dataset: %w", err)
		}
		command := exec.CommandContext(ctx, "zstd", "-d", "-c", "--no-progress", sourcePath)
		command.Stdout = destination
		var stderr bytes.Buffer
		command.Stderr = &stderr
		commandErr := command.Run()
		syncErr := destination.Sync()
		closeErr := destination.Close()
		if commandErr != nil || syncErr != nil || closeErr != nil {
			_ = os.Remove(destinationPath)
			return errors.Join(fmt.Errorf("restore compressed benchmark dataset: %w: %s", commandErr, strings.TrimSpace(stderr.String())), syncErr, closeErr)
		}
		return nil
	}
	// Canonical 数据库在生成后已 checkpoint、关闭并验证；直接字节复制既保持
	// SQLite 页布局不变，也避免每个 probe 再调用 sqlite3 backup 的额外开销。
	return copyStaticDataset(sourcePath, destinationPath)
}

// ResetDatasetClone gives every probe the same canonical database state. A
// failed high-rate probe may leave inbox backlog and partially advanced
// aggregates behind, so reusing its database would bias every later probe.
func ResetDatasetClone(ctx context.Context, sourcePath, destinationPath string) error {
	sourcePath = filepath.Clean(strings.TrimSpace(sourcePath))
	destinationPath = filepath.Clean(strings.TrimSpace(destinationPath))
	if sourcePath == "." || destinationPath == "." || sourcePath == destinationPath {
		return fmt.Errorf("dataset reset requires distinct source and destination paths")
	}
	for _, path := range []string{destinationPath + "-shm", destinationPath + "-wal", destinationPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove previous probe database %s: %w", path, err)
		}
	}
	if err := RestoreDataset(ctx, sourcePath, destinationPath); err != nil {
		return err
	}
	if err := DropFilePageCache(destinationPath); err != nil {
		_ = os.Remove(destinationPath)
		return fmt.Errorf("evict restored dataset page cache: %w", err)
	}
	return nil
}

func CompressDataset(ctx context.Context, sourcePath, destinationPath string, removeSource bool) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destinationPath) == "" {
		return fmt.Errorf("dataset compression source and destination are required")
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("compressed dataset already exists: %s", filepath.Clean(destinationPath))
	} else if !os.IsNotExist(err) {
		return err
	}
	output, err := exec.CommandContext(ctx, "zstd", "-T0", "-6", "--no-progress", "-o", destinationPath, sourcePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("compress benchmark dataset: %w: %s", err, strings.TrimSpace(string(output)))
	}
	output, err = exec.CommandContext(ctx, "zstd", "-t", "--no-progress", destinationPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify compressed benchmark dataset: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if removeSource {
		if err := os.Remove(sourcePath); err != nil {
			return fmt.Errorf("remove uncompressed dataset after verified compression: %w", err)
		}
	}
	return nil
}

type CgroupSample struct {
	At                 time.Time `json:"at"`
	CPUUsageUsec       int64     `json:"cpu_usage_usec"`
	CPUUserUsec        int64     `json:"cpu_user_usec"`
	CPUSystemUsec      int64     `json:"cpu_system_usec"`
	CPUThrottledUsec   int64     `json:"cpu_throttled_usec"`
	CPUNrThrottled     int64     `json:"cpu_nr_throttled"`
	MemoryCurrentBytes int64     `json:"memory_current_bytes"`
	MemoryPeakBytes    int64     `json:"memory_peak_bytes"`
	MemorySwapBytes    int64     `json:"memory_swap_bytes"`
	MemoryOOM          int64     `json:"memory_oom"`
	MemoryOOMKill      int64     `json:"memory_oom_kill"`
	IOReadBytes        int64     `json:"io_read_bytes"`
	IOWriteBytes       int64     `json:"io_write_bytes"`
	PIDsCurrent        int64     `json:"pids_current"`
}

func ReadCgroupLimits(path string) (CgroupLimits, error) {
	limits := CgroupLimits{}
	cpuMax, err := os.ReadFile(filepath.Join(path, "cpu.max"))
	if err != nil {
		return limits, fmt.Errorf("read cgroup cpu.max: %w", err)
	}
	fields := strings.Fields(string(cpuMax))
	if len(fields) != 2 || fields[0] == "max" {
		return limits, fmt.Errorf("cgroup cpu.max is not a finite quota: %q", strings.TrimSpace(string(cpuMax)))
	}
	limits.CPUQuotaUsec, err = strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return limits, fmt.Errorf("parse cgroup CPU quota: %w", err)
	}
	limits.CPUPeriodUsec, err = strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return limits, fmt.Errorf("parse cgroup CPU period: %w", err)
	}
	if limits.AllowedCPUs, err = ReadCgroupAllowedCPUs(path); err != nil {
		return limits, err
	}
	if limits.MemoryMaxBytes, limits.MemoryMaxUnlimited, err = readMemoryMaxFile(filepath.Join(path, "memory.max")); err != nil {
		return limits, err
	}
	if limits.MemorySwapMaxBytes, err = readFiniteIntegerFile(filepath.Join(path, "memory.swap.max")); err != nil {
		return limits, err
	}
	return limits, nil
}

func ReadCgroupAllowedCPUs(path string) (string, error) {
	allowed, err := os.ReadFile(filepath.Join(path, "cpuset.cpus.effective"))
	if err != nil {
		return "", fmt.Errorf("read cgroup effective CPUs: %w", err)
	}
	return strings.TrimSpace(string(allowed)), nil
}

func ValidateCgroupLimits(limits CgroupLimits, cpu, memoryMiB int, allowedCPUs string) error {
	if limits.CPUPeriodUsec <= 0 || limits.CPUQuotaUsec != int64(cpu)*limits.CPUPeriodUsec {
		return fmt.Errorf("effective CPU quota=%d/%d, want %d CPUs", limits.CPUQuotaUsec, limits.CPUPeriodUsec, cpu)
	}
	if limits.AllowedCPUs != allowedCPUs {
		return fmt.Errorf("effective AllowedCPUs=%q, want %q", limits.AllowedCPUs, allowedCPUs)
	}
	if memoryMiB == 0 {
		if !limits.MemoryMaxUnlimited {
			return fmt.Errorf("effective memory.max=%d, want unlimited", limits.MemoryMaxBytes)
		}
	} else {
		wantedMemory := int64(memoryMiB) * 1024 * 1024
		if limits.MemoryMaxUnlimited || limits.MemoryMaxBytes != wantedMemory {
			return fmt.Errorf("effective memory.max=%d unlimited=%t, want %d", limits.MemoryMaxBytes, limits.MemoryMaxUnlimited, wantedMemory)
		}
	}
	if limits.MemorySwapMaxBytes != 0 {
		return fmt.Errorf("effective memory.swap.max=%d, want 0", limits.MemorySwapMaxBytes)
	}
	return nil
}

func ReadCgroupSample(path string) (CgroupSample, error) {
	sample := CgroupSample{At: time.Now()}
	cpu, err := readKeyValueFile(filepath.Join(path, "cpu.stat"))
	if err != nil {
		return sample, err
	}
	sample.CPUUsageUsec = cpu["usage_usec"]
	sample.CPUUserUsec = cpu["user_usec"]
	sample.CPUSystemUsec = cpu["system_usec"]
	sample.CPUThrottledUsec = cpu["throttled_usec"]
	sample.CPUNrThrottled = cpu["nr_throttled"]
	if sample.MemoryCurrentBytes, err = readIntegerFile(filepath.Join(path, "memory.current")); err != nil {
		return sample, err
	}
	if sample.MemoryPeakBytes, err = readIntegerFile(filepath.Join(path, "memory.peak")); err != nil {
		return sample, err
	}
	if sample.MemorySwapBytes, err = readIntegerFile(filepath.Join(path, "memory.swap.current")); err != nil {
		return sample, err
	}
	memoryEvents, err := readKeyValueFile(filepath.Join(path, "memory.events"))
	if err != nil {
		return sample, err
	}
	sample.MemoryOOM = memoryEvents["oom"]
	sample.MemoryOOMKill = memoryEvents["oom_kill"]
	if sample.PIDsCurrent, err = readIntegerFile(filepath.Join(path, "pids.current")); err != nil {
		return sample, err
	}
	ioData, err := os.ReadFile(filepath.Join(path, "io.stat"))
	if err != nil {
		return sample, fmt.Errorf("read cgroup io.stat: %w", err)
	}
	for _, line := range strings.Split(string(ioData), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			parsed, _ := strconv.ParseInt(value, 10, 64)
			switch key {
			case "rbytes":
				sample.IOReadBytes += parsed
			case "wbytes":
				sample.IOWriteBytes += parsed
			}
		}
	}
	return sample, nil
}

func readKeyValueFile(path string) (map[string]int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	values := map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr == nil {
			values[fields[0]] = value
		}
	}
	return values, nil
}

func readIntegerFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return value, nil
}

func readFiniteIntegerFile(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, fmt.Errorf("%s is unlimited", filepath.Base(path))
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return parsed, nil
}

func readMemoryMaxFile(path string) (int64, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, true, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return parsed, false, nil
}

type CgroupSampler struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	last   CgroupSample
	peak   CgroupSample
	err    error
}

func StartCgroupSampler(parent context.Context, path, outputPath string, interval time.Duration) (*CgroupSampler, error) {
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return nil, fmt.Errorf("create cgroup sample directory: %w", err)
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create cgroup samples: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	sampler := &CgroupSampler{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(sampler.done)
		defer file.Close()
		encoder := json.NewEncoder(file)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			sample, sampleErr := ReadCgroupSample(path)
			if sampleErr == nil {
				_ = encoder.Encode(sample)
				sampler.mu.Lock()
				sampler.last = sample
				if sample.MemoryCurrentBytes > sampler.peak.MemoryCurrentBytes {
					sampler.peak.MemoryCurrentBytes = sample.MemoryCurrentBytes
				}
				if sample.MemoryPeakBytes > sampler.peak.MemoryPeakBytes {
					sampler.peak.MemoryPeakBytes = sample.MemoryPeakBytes
				}
				sampler.mu.Unlock()
			} else if !errors.Is(sampleErr, os.ErrNotExist) {
				sampler.mu.Lock()
				if sampler.err == nil {
					sampler.err = sampleErr
				}
				sampler.mu.Unlock()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return sampler, nil
}

func (sampler *CgroupSampler) Stop() (CgroupSample, CgroupSample, error) {
	if sampler == nil {
		return CgroupSample{}, CgroupSample{}, nil
	}
	sampler.cancel()
	<-sampler.done
	sampler.mu.Lock()
	defer sampler.mu.Unlock()
	return sampler.last, sampler.peak, sampler.err
}

func CopyFile(sourcePath, destinationPath string, mode os.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	return errors.Join(copyErr, closeErr)
}
