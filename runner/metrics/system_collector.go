package metrics

import (
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/jsonrpc-bench/runner/types"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemCollector collects system metrics during benchmark execution
type SystemCollector struct {
	mu            sync.RWMutex
	metrics       []types.SystemMetrics
	isCollecting  bool
	stopCh        chan struct{}
	interval      time.Duration
	pid           int32
	proc          *process.Process
	lastNetStats  net.IOCountersStat
	lastDiskStats disk.IOCountersStat
}

// NewSystemCollector creates a new system metrics collector
func NewSystemCollector(interval time.Duration) (*SystemCollector, error) {
	pid := int32(os.Getpid())
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, err
	}

	return &SystemCollector{
		interval: interval,
		pid:      pid,
		proc:     proc,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start begins collecting system metrics
func (sc *SystemCollector) Start() error {
	sc.mu.Lock()
	if sc.isCollecting {
		sc.mu.Unlock()
		return nil
	}
	sc.isCollecting = true
	sc.metrics = make([]types.SystemMetrics, 0)
	sc.mu.Unlock()

	// Get initial network and disk stats
	if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
		sc.lastNetStats = netStats[0]
	}

	if diskStats, err := disk.IOCounters(); err == nil {
		for _, stat := range diskStats {
			sc.lastDiskStats = stat
			break
		}
	}

	go sc.collect()
	return nil
}

// Stop stops collecting system metrics
func (sc *SystemCollector) Stop() {
	sc.mu.Lock()
	if !sc.isCollecting {
		sc.mu.Unlock()
		return
	}
	sc.isCollecting = false
	sc.mu.Unlock()

	close(sc.stopCh)
}

// GetMetrics returns collected metrics
func (sc *SystemCollector) GetMetrics() []types.SystemMetrics {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	result := make([]types.SystemMetrics, len(sc.metrics))
	copy(result, sc.metrics)
	return result
}

// collect runs the metric collection loop
func (sc *SystemCollector) collect() {
	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			metric := sc.collectMetric()
			sc.mu.Lock()
			sc.metrics = append(sc.metrics, metric)
			sc.mu.Unlock()
		case <-sc.stopCh:
			return
		}
	}
}

// collectMetric collects a single metric snapshot
func (sc *SystemCollector) collectMetric() types.SystemMetrics {
	metric := types.SystemMetrics{
		GoroutineCount: runtime.NumGoroutine(),
	}

	// CPU usage
	if cpuPercent, err := sc.proc.CPUPercent(); err == nil {
		metric.CPUUsage = cpuPercent
	}

	// Memory usage
	if memInfo, err := mem.VirtualMemory(); err == nil {
		metric.MemoryPercent = memInfo.UsedPercent
		metric.MemoryUsage = float64(memInfo.Used) / 1024 / 1024 // Convert to MB
	}

	// Process memory
	if procMem, err := sc.proc.MemoryInfo(); err == nil {
		metric.MemoryUsage = float64(procMem.RSS) / 1024 / 1024 // Convert to MB
	}

	// Network I/O
	if netStats, err := net.IOCounters(false); err == nil && len(netStats) > 0 {
		current := netStats[0]
		metric.NetworkBytesSent = int64(current.BytesSent - sc.lastNetStats.BytesSent)
		metric.NetworkBytesRecv = int64(current.BytesRecv - sc.lastNetStats.BytesRecv)
		sc.lastNetStats = current
	}

	// Disk I/O
	if diskStats, err := disk.IOCounters(); err == nil {
		for _, stat := range diskStats {
			metric.DiskIORead = int64(stat.ReadBytes - sc.lastDiskStats.ReadBytes)
			metric.DiskIOWrite = int64(stat.WriteBytes - sc.lastDiskStats.WriteBytes)
			sc.lastDiskStats = stat
			break
		}
	}

	// Open connections (TCP)
	if connections, err := sc.proc.Connections(); err == nil {
		metric.OpenConnections = int64(len(connections))
	}

	return metric
}

// GetAverageMetrics calculates average metrics over the collection period
func (sc *SystemCollector) GetAverageMetrics() types.SystemMetrics {
	metrics := sc.GetMetrics()
	if len(metrics) == 0 {
		return types.SystemMetrics{}
	}

	avg := types.SystemMetrics{}
	var cpuSum, memSum, memPercentSum float64
	var netSentSum, netRecvSum, diskReadSum, diskWriteSum, connSum int64
	var goroutineSum int

	for _, m := range metrics {
		cpuSum += m.CPUUsage
		memSum += m.MemoryUsage
		memPercentSum += m.MemoryPercent
		netSentSum += m.NetworkBytesSent
		netRecvSum += m.NetworkBytesRecv
		diskReadSum += m.DiskIORead
		diskWriteSum += m.DiskIOWrite
		connSum += m.OpenConnections
		goroutineSum += m.GoroutineCount
	}

	n := float64(len(metrics))
	avg.CPUUsage = cpuSum / n
	avg.MemoryUsage = memSum / n
	avg.MemoryPercent = memPercentSum / n
	avg.NetworkBytesSent = netSentSum / int64(n)
	avg.NetworkBytesRecv = netRecvSum / int64(n)
	avg.DiskIORead = diskReadSum / int64(n)
	avg.DiskIOWrite = diskWriteSum / int64(n)
	avg.OpenConnections = connSum / int64(n)
	avg.GoroutineCount = goroutineSum / int(n)

	return avg
}

// GetEnvironmentInfo collects static environment information
func GetEnvironmentInfo() types.EnvironmentInfo {
	env := types.EnvironmentInfo{
		OS:           runtime.GOOS,
		Architecture: runtime.GOARCH,
		GoVersion:    runtime.Version(),
		CPUCores:     runtime.NumCPU(),
	}

	// CPU model
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		env.CPUModel = cpuInfo[0].ModelName
	}

	// Total memory
	if memInfo, err := mem.VirtualMemory(); err == nil {
		env.TotalMemoryGB = float64(memInfo.Total) / 1024 / 1024 / 1024
	}

	// Network type (simplified - could be enhanced)
	env.NetworkType = "ethernet" // Default assumption

	return env
}
