//go:build linux

package qualitygate

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const linuxResourceSourceLabel = "linux-procfs"

func platformHostResourceSource() hostResourceSource {
	host := ResourceSampleHost{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		CPUDenominator:   "cpu percentages are 0-100 where 100 means every logical cpu is busy",
		Units:            "memory and swap are bytes",
		CPUBasis:         "linux: /proc/stat cumulative cpu jiffies differenced between consecutive samples; the first sample after start has no cpu usage window; busy = user + nice + system + irq + softirq + steal; idle includes iowait; guest and guest_nice are already included in user and nice and are excluded from the total to avoid double counting",
		MemoryDefinition: "linux: /proc/meminfo; used = MemTotal - MemAvailable; available = MemAvailable, the kernel estimate including reclaimable cache",
		SwapDefinition:   "linux: /proc/meminfo; used = SwapTotal - SwapFree; SwapCached pages count as used; SwapTotal 0 means no swap is configured",
	}
	source := hostResourceSource{
		label: linuxResourceSourceLabel, supported: true, host: host, read: readLinuxHostResources,
	}
	if err := probeLinuxHost(&source); err != nil {
		source.supported = false
		source.reason = "memory: " + err.Error()
	}
	return source
}

func probeLinuxHost(source *hostResourceSource) error {
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return err
	}
	source.host.LogicalCPUs = countLinuxLogicalCPUs(string(stat))
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return err
	}
	source.host.TotalMemoryBytes = parseLinuxMeminfo(string(meminfo))["MemTotal"] * 1024
	if source.host.LogicalCPUs == 0 || source.host.TotalMemoryBytes == 0 {
		return fmt.Errorf("procfs did not report the host cpu or memory inventory")
	}
	return nil
}

func countLinuxLogicalCPUs(stat string) int {
	cpus := 0
	for _, line := range strings.Split(stat, "\n") {
		if len(line) < 4 || line[:3] != "cpu" || line[3] < '0' || line[3] > '9' {
			continue
		}
		cpus++
	}
	return cpus
}

func readLinuxHostResources() hostResourceReading {
	reading := hostResourceReading{}
	readLinuxCPU(&reading)
	readLinuxMemory(&reading)
	return reading
}

func readLinuxCPU(reading *hostResourceReading) {
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		reading.problems = append(reading.problems, "cpu: "+err.Error())
		return
	}
	ticks, err := parseLinuxCPUTicks(string(stat))
	if err != nil {
		reading.problems = append(reading.problems, "cpu: "+err.Error())
		return
	}
	reading.cpuTicks = ticks
}

func parseLinuxCPUTicks(stat string) (*cpuTicksReading, error) {
	for _, line := range strings.Split(stat, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return nil, fmt.Errorf("/proc/stat aggregate cpu line is truncated")
		}
		values := make([]uint64, len(fields)-1)
		for index, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("/proc/stat aggregate cpu line is not numeric")
			}
			values[index] = value
		}
		return linuxTicksFromCounters(values), nil
	}
	return nil, fmt.Errorf("/proc/stat has no aggregate cpu line")
}

func linuxTicksFromCounters(values []uint64) *cpuTicksReading {
	ticks := &cpuTicksReading{
		user:   linuxCounter(values, 0) + linuxCounter(values, 1),
		idle:   linuxCounter(values, 3) + linuxCounter(values, 4),
		system: linuxCounter(values, 2) + linuxCounter(values, 5) + linuxCounter(values, 6) + linuxCounter(values, 7),
	}
	ticks.total = ticks.user + ticks.system + ticks.idle
	return ticks
}

func linuxCounter(values []uint64, index int) uint64 {
	if index >= len(values) {
		return 0
	}
	return values[index]
}

func readLinuxMemory(reading *hostResourceReading) {
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		reading.problems = append(reading.problems, "memory: "+err.Error())
		reading.problems = append(reading.problems, "swap: "+err.Error())
		return
	}
	values := parseLinuxMeminfo(string(meminfo))
	total, hasTotal := values["MemTotal"]
	available, hasAvailable := values["MemAvailable"]
	if !hasTotal || !hasAvailable || total == 0 {
		reading.problems = append(reading.problems, "memory: /proc/meminfo lacks MemTotal or MemAvailable")
	} else {
		totalBytes, availableBytes := total*1024, available*1024
		reading.memory = &MemorySample{UsedBytes: totalBytes - availableBytes, AvailableBytes: availableBytes}
	}
	swapTotal, hasSwapTotal := values["SwapTotal"]
	swapFree, hasSwapFree := values["SwapFree"]
	if !hasSwapTotal || !hasSwapFree {
		reading.problems = append(reading.problems, "swap: /proc/meminfo lacks SwapTotal or SwapFree")
		return
	}
	totalBytes, freeBytes := swapTotal*1024, swapFree*1024
	reading.swap = &SwapSample{TotalBytes: totalBytes, UsedBytes: totalBytes - freeBytes, FreeBytes: freeBytes}
}

func parseLinuxMeminfo(meminfo string) map[string]uint64 {
	values := map[string]uint64{}
	for _, line := range strings.Split(meminfo, "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		values[strings.TrimSpace(key)] = value
	}
	return values
}
