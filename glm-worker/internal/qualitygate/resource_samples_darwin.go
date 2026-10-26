//go:build darwin

package qualitygate

import (
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const darwinResourceCommandTimeout = 3 * time.Second

const darwinCPUTopWindow = 2 * time.Second

const darwinCPUTopTimeout = 10 * time.Second

const darwinResourceSourceLabel = "darwin-sysctl-vm-stat-top"

func platformHostResourceSource() hostResourceSource {
	host := ResourceSampleHost{
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		CPUDenominator:   "cpu percentages are 0-100 where 100 means every logical cpu is busy",
		Units:            "memory and swap are bytes; darwin swap values are quantized to megabytes by their source",
		CPUBasis:         "darwin: sample.at stamps the start of the reading pass; CPU usage is the second sample of /usr/bin/top -l 2 -s 2, invoked after the memory and swap reads, so its interval of roughly 2 seconds starts after sample.at by that read time and command scheduling; window_ms records the nominal top delay, not a measured interval; busy = user + sys as reported by top",
		MemoryDefinition: "darwin: /usr/bin/vm_stat page counts; used = (anonymous + wired down + occupied by compressor) * page size; available = (free + speculative) * page size; clean file cache is excluded from available although the system can reclaim it; raw page counts are recorded for re-derivation",
		SwapDefinition:   "darwin: /usr/sbin/sysctl -n vm.swapusage formatted output; used = total - free as reported; values are megabyte-quantized",
	}
	source := hostResourceSource{
		label: darwinResourceSourceLabel, supported: true, host: host, read: readDarwinHostResources,
	}
	source.host.LogicalCPUs = darwinLogicalCPUs()
	source.host.TotalMemoryBytes = darwinTotalMemory()
	if _, err := darwinMemorySample(); err != nil {
		source.supported = false
		source.reason = "memory: vm_stat is unavailable on this host"
	}
	return source
}

func readDarwinHostResources() hostResourceReading {
	reading := hostResourceReading{}
	memory, memoryErr := darwinMemorySample()
	if memoryErr != nil {
		reading.problems = append(reading.problems, "memory: "+memoryErr.Error())
	} else {
		reading.memory = memory
	}
	swap, swapErr := darwinSwapSample()
	if swapErr != nil {
		reading.problems = append(reading.problems, "swap: "+swapErr.Error())
	} else {
		reading.swap = swap
	}
	percent, cpuErr := darwinCPUPercent()
	if cpuErr != nil {
		reading.problems = append(reading.problems, "cpu: "+cpuErr.Error())
	} else {
		reading.cpuPercent = percent
	}
	return reading
}

func darwinMemorySample() (*MemorySample, error) {
	out, err := darwinCommandOutput(darwinResourceCommandTimeout, "/usr/bin/vm_stat")
	if err != nil {
		return nil, err
	}
	return darwinMemoryFromVMStat(out)
}

func darwinMemoryFromVMStat(out string) (*MemorySample, error) {
	pageSize, pages, err := parseDarwinVMStat(out)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"Anonymous pages", "Pages wired down", "Pages occupied by compressor", "Pages free", "Pages speculative"} {
		if _, ok := pages[key]; !ok {
			return nil, fmt.Errorf("vm_stat lacks the %s counter", key)
		}
	}
	used := (pages["Anonymous pages"] + pages["Pages wired down"] + pages["Pages occupied by compressor"]) * pageSize
	available := (pages["Pages free"] + pages["Pages speculative"]) * pageSize
	return &MemorySample{UsedBytes: used, AvailableBytes: available, RawPages: pages}, nil
}

func parseDarwinVMStat(out string) (uint64, map[string]uint64, error) {
	pageSize := uint64(0)
	pages := map[string]uint64{}
	for _, line := range strings.Split(out, "\n") {
		if size, ok := parseDarwinVMStatPageSize(line); ok {
			pageSize = size
			continue
		}
		if name, count, ok := parseDarwinVMStatPageLine(line); ok {
			pages[name] = count
		}
	}
	if pageSize == 0 || len(pages) == 0 {
		return 0, nil, fmt.Errorf("vm_stat output was not recognized")
	}
	return pageSize, pages, nil
}

func parseDarwinVMStatPageSize(line string) (uint64, bool) {
	if !strings.Contains(line, "page size of") || !strings.Contains(line, "bytes)") {
		return 0, false
	}
	for _, field := range strings.Fields(line) {
		size, err := strconv.ParseUint(field, 10, 64)
		if err == nil && size > 0 {
			return size, true
		}
	}
	return 0, false
}

func parseDarwinVMStatPageLine(line string) (string, uint64, bool) {
	name, rest, ok := strings.Cut(line, ":")
	if !ok {
		return "", 0, false
	}
	count, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimSpace(rest), "."), 10, 64)
	if err != nil {
		return "", 0, false
	}
	return strings.Trim(strings.TrimSpace(name), `"`), count, true
}

func darwinSwapSample() (*SwapSample, error) {
	out, err := darwinCommandOutput(darwinResourceCommandTimeout, "/usr/sbin/sysctl", "-n", "vm.swapusage")
	if err != nil {
		return nil, err
	}
	return parseDarwinSwapUsage(out)
}

func parseDarwinSwapUsage(value string) (*SwapSample, error) {
	fields := strings.Fields(value)
	sample := SwapSample{}
	seen := 0
	for index := 0; index+2 < len(fields); index++ {
		switch fields[index] {
		case "total", "used", "free":
		default:
			continue
		}
		if fields[index+1] != "=" {
			continue
		}
		bytes, err := parseDarwinSwapValue(fields[index+2])
		if err != nil {
			continue
		}
		switch fields[index] {
		case "total":
			sample.TotalBytes = bytes
		case "used":
			sample.UsedBytes = bytes
		case "free":
			sample.FreeBytes = bytes
		}
		seen++
	}
	if seen < 3 {
		return nil, fmt.Errorf("vm.swapusage value %q was not recognized", value)
	}
	return &sample, nil
}

func parseDarwinSwapValue(token string) (uint64, error) {
	if !strings.HasSuffix(token, "M") {
		return 0, fmt.Errorf("swap value %q is not megabyte-quantized", token)
	}
	megabytes, err := strconv.ParseFloat(strings.TrimSuffix(token, "M"), 64)
	if err != nil || megabytes < 0 {
		return 0, fmt.Errorf("swap value %q is not numeric", token)
	}
	return uint64(megabytes*1024*1024 + 0.5), nil
}

func darwinCPUPercent() (*cpuPercentReading, error) {
	delay := strconv.Itoa(int(darwinCPUTopWindow.Seconds()))
	out, err := darwinCommandOutput(darwinCPUTopTimeout, "/usr/bin/top", "-l", "2", "-s", delay, "-n", "0")
	if err != nil {
		return nil, err
	}
	percent, err := parseDarwinCPUSamples(out)
	if err != nil {
		return nil, err
	}
	percent.windowMS = darwinCPUTopWindow.Milliseconds()
	return percent, nil
}

func parseDarwinCPUSamples(out string) (*cpuPercentReading, error) {
	var latest *cpuPercentReading
	for _, line := range strings.Split(out, "\n") {
		if percent, ok := parseDarwinCPULine(line); ok {
			latest = percent
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("top output lacked a CPU usage line")
	}
	return latest, nil
}

func parseDarwinCPULine(line string) (*cpuPercentReading, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "CPU usage:") {
		return nil, false
	}
	fields := strings.Fields(strings.TrimPrefix(trimmed, "CPU usage:"))
	reading := cpuPercentReading{}
	seen := 0
	for index := 0; index+1 < len(fields); index++ {
		value, err := strconv.ParseFloat(strings.TrimSuffix(fields[index], "%"), 64)
		if err != nil || value < 0 {
			continue
		}
		switch strings.TrimSuffix(fields[index+1], ",") {
		case "user":
			reading.user = value
		case "sys":
			reading.system = value
		case "idle":
			reading.idle = value
		default:
			continue
		}
		seen++
	}
	if seen < 3 {
		return nil, false
	}
	reading.busy = roundPercent(reading.user + reading.system)
	return &reading, true
}

func darwinLogicalCPUs() int {
	if value, err := syscall.Sysctl("hw.ncpu"); err == nil {
		if cpus := darwinSysctlInt(value); cpus > 0 {
			return cpus
		}
	}
	out, err := darwinCommandOutput(darwinResourceCommandTimeout, "/usr/bin/hostinfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		if cpus, ok := parseDarwinLogicalCPULine(line); ok {
			return cpus
		}
	}
	return 0
}

func parseDarwinLogicalCPULine(line string) (int, bool) {
	if !strings.Contains(line, "processors are logically available") {
		return 0, false
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0, false
	}
	cpus, err := strconv.Atoi(fields[0])
	if err != nil || cpus <= 0 {
		return 0, false
	}
	return cpus, true
}

func darwinTotalMemory() uint64 {
	if value, err := syscall.Sysctl("hw.memsize"); err == nil {
		if total := darwinSysctlUint64(value); total > 0 {
			return total
		}
	}
	out, err := darwinCommandOutput(darwinResourceCommandTimeout, "/usr/bin/memory_pressure")
	if err != nil {
		return 0
	}
	return parseDarwinMemoryPressureTotal(out)
}

func parseDarwinMemoryPressureTotal(out string) uint64 {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "The system has ") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "The system has "))
		if len(fields) == 0 {
			continue
		}
		total, err := strconv.ParseUint(fields[0], 10, 64)
		if err == nil && total > 0 {
			return total
		}
	}
	return 0
}

func darwinSysctlInt(value string) int {
	if len(value) != 4 {
		return 0
	}
	return int(int32(binary.LittleEndian.Uint32([]byte(value))))
}

func darwinSysctlUint64(value string) uint64 {
	if len(value) != 8 {
		return 0
	}
	return binary.LittleEndian.Uint64([]byte(value))
}

func darwinCommandOutput(timeout time.Duration, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
