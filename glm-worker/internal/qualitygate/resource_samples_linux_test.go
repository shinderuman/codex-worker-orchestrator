//go:build linux

package qualitygate

import (
	"testing"
)

const linuxProcStatFixture = "cpu  74608 2520 24433 1117073 6176 4054 0 0 0 0\n" +
	"cpu0 37212 1260 12216 558536 3088 2027 0 0 0 0\n" +
	"cpu1 37396 1260 12217 558537 3088 2027 0 0 0 0\n" +
	"intr 2821\n" +
	"ctxt 12345\n"

const linuxMeminfoFixture = "MemTotal:       16384000 kB\n" +
	"MemFree:         1024000 kB\n" +
	"MemAvailable:    8192000 kB\n" +
	"Buffers:          204800 kB\n" +
	"Cached:          4096000 kB\n" +
	"SwapCached:        16384 kB\n" +
	"SwapTotal:       2097152 kB\n" +
	"SwapFree:        1048576 kB\n"

func TestParseLinuxCPUTicks(t *testing.T) {
	ticks, err := parseLinuxCPUTicks(linuxProcStatFixture)
	if err != nil {
		t.Fatal(err)
	}
	if ticks.user != 74608+2520 || ticks.system != 24433+4054 || ticks.idle != 1117073+6176 || ticks.total != 74608+2520+24433+1117073+6176+4054 {
		t.Fatalf("ticks = %#v", ticks)
	}
	guest, err := parseLinuxCPUTicks("cpu  100 10 50 700 20 5 5 10 40 4\n")
	if err != nil {
		t.Fatal(err)
	}
	if guest.user != 110 || guest.system != 70 || guest.idle != 720 {
		t.Fatalf("guest fixture buckets = %#v", guest)
	}
	if guest.total != guest.user+guest.system+guest.idle || guest.total != 900 {
		t.Fatalf("guest time was double counted into the total: %#v", guest)
	}
	if _, err := parseLinuxCPUTicks("intr 2821\n"); err == nil {
		t.Fatal("stat without an aggregate cpu line was accepted")
	}
	if _, err := parseLinuxCPUTicks("cpu 1 2 3\n"); err == nil {
		t.Fatal("truncated aggregate cpu line was accepted")
	}
}

func TestParseLinuxMeminfo(t *testing.T) {
	values := parseLinuxMeminfo(linuxMeminfoFixture)
	if values["MemTotal"] != 16384000 || values["MemAvailable"] != 8192000 || values["SwapTotal"] != 2097152 || values["SwapFree"] != 1048576 {
		t.Fatalf("meminfo values = %#v", values)
	}
	if _, ok := values["HugePages_Total"]; ok {
		t.Fatalf("absent meminfo key was fabricated: %#v", values)
	}
}

func TestCountLinuxLogicalCPUs(t *testing.T) {
	if cpus := countLinuxLogicalCPUs(linuxProcStatFixture); cpus != 2 {
		t.Fatalf("logical cpus = %d", cpus)
	}
	if cpus := countLinuxLogicalCPUs("cpu  1 2 3 4 5 6 7 8 9 10\nintr 1\n"); cpus != 0 {
		t.Fatalf("aggregate-only stat counted per-cpu lines: %d", cpus)
	}
}

func TestLinuxHostResourceSourceDeclaresDefinitions(t *testing.T) {
	source := platformHostResourceSource()
	if source.label != linuxResourceSourceLabel || !source.supported {
		t.Fatalf("linux source = %#v", source)
	}
	for _, definition := range []string{source.host.CPUBasis, source.host.MemoryDefinition, source.host.SwapDefinition, source.host.CPUDenominator, source.host.Units} {
		if definition == "" {
			t.Fatalf("linux host context lacks a metric definition: %#v", source.host)
		}
	}
	if source.host.LogicalCPUs == 0 || source.host.TotalMemoryBytes == 0 {
		t.Fatalf("linux host inventory is unpopulated: %#v", source.host)
	}
}
