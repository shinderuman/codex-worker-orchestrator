//go:build darwin

package qualitygate

import (
	"encoding/binary"
	"strings"
	"testing"
)

const darwinVMStatFixture = "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
	"Pages free:                                 8318.\n" +
	"Pages active:                              291976.\n" +
	"Pages speculative:                           347.\n" +
	"Pages wired down:                          356973.\n" +
	"Pages purgeable:                             2724.\n" +
	"\"Translation faults\":                35421002997.\n" +
	"File-backed pages:                          188689.\n" +
	"Anonymous pages:                            392947.\n" +
	"Pages stored in compressor:                 423030.\n" +
	"Pages occupied by compressor:                68309.\n"

func TestDarwinMemoryFromVMStatDerivesUsedAndAvailable(t *testing.T) {
	memory, err := darwinMemoryFromVMStat(darwinVMStatFixture)
	if err != nil {
		t.Fatal(err)
	}
	used := uint64(392947+356973+68309) * 16384
	available := uint64(8318+347) * 16384
	if memory.UsedBytes != used || memory.AvailableBytes != available {
		t.Fatalf("derived memory = %#v", memory)
	}
	if memory.RawPages["Pages free"] != 8318 || memory.RawPages["Pages occupied by compressor"] != 68309 {
		t.Fatalf("raw pages = %#v", memory.RawPages)
	}
	if _, ok := memory.RawPages["Translation faults"]; !ok {
		t.Fatalf("quoted vm_stat counters were dropped instead of normalized: %#v", memory.RawPages)
	}
}

func TestDarwinMemoryFromVMStatRejectsIncompleteOutput(t *testing.T) {
	if _, err := darwinMemoryFromVMStat("Mach Virtual Memory Statistics: (page size of 16384 bytes)\nPages free: 10.\n"); err == nil {
		t.Fatal("incomplete vm_stat output was accepted")
	}
	if _, err := darwinMemoryFromVMStat("unrecognized output\n"); err == nil {
		t.Fatal("unrecognized vm_stat output was accepted")
	}
}

func TestParseDarwinSwapUsage(t *testing.T) {
	swap, err := parseDarwinSwapUsage("total = 6144.00M  used = 128.00M  free = 6016.00M")
	if err != nil {
		t.Fatal(err)
	}
	if swap.TotalBytes != 6144<<20 || swap.UsedBytes != 128<<20 || swap.FreeBytes != 6016<<20 {
		t.Fatalf("swap = %#v", swap)
	}
	if _, err := parseDarwinSwapUsage("total = 6144.00M  used = 128.00M"); err == nil {
		t.Fatal("partial swap usage was accepted")
	}
}

func TestParseDarwinCPULine(t *testing.T) {
	percent, ok := parseDarwinCPULine("CPU usage: 5.70% user, 6.51% sys, 87.77% idle")
	if !ok {
		t.Fatal("cpu usage line was not parsed")
	}
	if percent.busy != 12.21 || percent.user != 5.70 || percent.system != 6.51 || percent.idle != 87.77 {
		t.Fatalf("cpu percents = %#v", percent)
	}
	if _, ok := parseDarwinCPULine("CPU usage: 5.70% user, 6.51% sys"); ok {
		t.Fatal("partial cpu usage line was accepted")
	}
	if _, ok := parseDarwinCPULine("Processes: 512 total, 3 running"); ok {
		t.Fatal("unrelated line was accepted")
	}
}

func TestParseDarwinCPUSamplesUsesIntervalSample(t *testing.T) {
	out := "Processes: 512 total, 3 running\n" +
		"CPU usage: 7.32% user, 5.11% sys, 87.56% idle, 0idw, 0.00% iowait\n" +
		"Load Avg: 1.85, 1.92, 1.84\n" +
		"CPU usage: 22.41% user, 19.13% sys, 58.46% idle, 0idw, 0.00% iowait\n"
	percent, err := parseDarwinCPUSamples(out)
	if err != nil {
		t.Fatal(err)
	}
	if percent.busy != 41.54 || percent.user != 22.41 || percent.system != 19.13 || percent.idle != 58.46 {
		t.Fatalf("cpu percents kept the since-boot first sample: %#v", percent)
	}
	if _, err := parseDarwinCPUSamples("Processes: 512 total, 3 running\n"); err == nil {
		t.Fatal("output without a CPU usage line was accepted")
	}
}

func TestParseDarwinHostInventoryLines(t *testing.T) {
	if cpus, ok := parseDarwinLogicalCPULine("8 processors are logically available."); !ok || cpus != 8 {
		t.Fatalf("logical cpu line = %d %v", cpus, ok)
	}
	if cpus, ok := parseDarwinLogicalCPULine("8 processors are physically available."); ok {
		t.Fatalf("physical cpu line was accepted: %d", cpus)
	}
	if total := parseDarwinMemoryPressureTotal("The system has 17179869184 (1048576 pages with a page size of 16384).\n"); total != 17179869184 {
		t.Fatalf("memory_pressure total = %d", total)
	}
	if total := parseDarwinMemoryPressureTotal("Stats:\nPages free: 5838\n"); total != 0 {
		t.Fatalf("memory_pressure fallback = %d", total)
	}
}

func TestDarwinSysctlDecoding(t *testing.T) {
	quad := make([]byte, 8)
	binary.LittleEndian.PutUint64(quad, 17179869184)
	if value := darwinSysctlUint64(string(quad)); value != 17179869184 {
		t.Fatalf("quad sysctl = %d", value)
	}
	quadInt := make([]byte, 4)
	binary.LittleEndian.PutUint32(quadInt, 8)
	if value := darwinSysctlInt(string(quadInt)); value != 8 {
		t.Fatalf("int sysctl = %d", value)
	}
	if value := darwinSysctlUint64("short"); value != 0 {
		t.Fatalf("truncated sysctl = %d", value)
	}
}

func TestDarwinHostResourceSourceDeclaresDefinitions(t *testing.T) {
	source := platformHostResourceSource()
	if source.label != darwinResourceSourceLabel || !source.supported {
		if source.reason == "" {
			t.Fatalf("darwin source neither supported nor reasoned: %#v", source)
		}
	}
	for _, definition := range []string{source.host.CPUBasis, source.host.MemoryDefinition, source.host.SwapDefinition, source.host.CPUDenominator, source.host.Units} {
		if definition == "" {
			t.Fatalf("darwin host context lacks a metric definition: %#v", source.host)
		}
	}
	if !strings.Contains(source.host.SwapDefinition, "megabyte") {
		t.Fatalf("darwin swap precision is undisclosed: %s", source.host.SwapDefinition)
	}
	if !strings.Contains(source.host.SwapDefinition, "sysctl -n vm.swapusage") {
		t.Fatalf("darwin swap definition hides the formatted sysctl source: %s", source.host.SwapDefinition)
	}
	if !strings.Contains(source.host.CPUBasis, "second sample") || !strings.Contains(source.host.CPUBasis, "nominal") {
		t.Fatalf("darwin cpu basis hides the interval source or the nominal window: %s", source.host.CPUBasis)
	}
	if strings.Contains(source.host.CPUBasis, "ending at") {
		t.Fatalf("darwin cpu basis claims the interval ends at the sample timestamp: %s", source.host.CPUBasis)
	}
}
