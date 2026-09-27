package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Capacity struct {
	Host        string    `json:"host"`
	CollectedAt time.Time `json:"collectedAt"`
	MemTotalMB  int       `json:"memTotalMB"`
	MemFreeMB   int       `json:"memFreeMB"`
	CPUs        int       `json:"cpus"`
	Load1       float64   `json:"load1"`
	Slots       int       `json:"slots"`
}

// Change-detection buckets: a capacity event fires only when slots or these coarse values move.
const (
	memBucketMB = 256
	loadStep    = 0.5
)

func (c Capacity) changeKey() interface{} {
	return []interface{}{c.Host, c.MemTotalMB, c.CPUs, c.Slots, c.MemFreeMB / memBucketMB, math.Round(c.Load1 / loadStep)}
}

// slots = max(0, min(floor((free-reserve)/mem), floor(cpus/gate.cpus), maxSlots)); CAD_SLOTS overrides.
func slots(memFreeMB, cpus int, g Gate) int {
	if n, err := strconv.Atoi(os.Getenv("CAD_SLOTS")); err == nil {
		return n
	}
	if g.MemoryMB <= 0 || g.CPUs <= 0 {
		return 0 // no usable policy
	}
	n := int(math.Floor(float64(memFreeMB-g.ReserveMB) / float64(g.MemoryMB)))
	if c := int(math.Floor(float64(cpus) / g.CPUs)); c < n {
		n = c
	}
	if g.MaxSlots != nil && *g.MaxSlots < n {
		n = *g.MaxSlots
	}
	if n < 0 {
		n = 0
	}
	return n
}

func collectCapacity() (interface{}, error) {
	var total, free int
	var load float64
	var err error
	if runtime.GOOS == "linux" {
		total, free, load, err = readLinux()
	} else {
		total, free, load, err = readDarwin()
	}
	if err != nil {
		return nil, err
	}
	p, _ := currentPolicy() // error already reported by the policy collector
	host, _ := os.Hostname()
	cpus := runtime.NumCPU()
	return Capacity{host, time.Now().UTC(), total, free, cpus, load, slots(free, cpus, p.Gate)}, nil
}

// kv parses "Key: value" lines; values keep only their first field, trailing '.' removed.
func kv(b []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		p := strings.SplitN(sc.Text(), ":", 2)
		if len(p) == 2 && len(strings.Fields(p[1])) > 0 {
			m[strings.TrimSpace(p[0])] = strings.TrimSuffix(strings.Fields(p[1])[0], ".")
		}
	}
	return m
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func readLinux() (int, int, float64, error) {
	mi, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, err
	}
	la, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0, err
	}
	m := kv(mi) // values in kB
	load, err := strconv.ParseFloat(strings.Fields(string(la))[0], 64)
	return atoi(m["MemTotal"]) / 1024, atoi(m["MemAvailable"]) / 1024, load, err
}

func readDarwin() (int, int, float64, error) {
	out, err := exec.Command("sysctl", "-n", "hw.memsize", "vm.loadavg").Output()
	if err != nil {
		return 0, 0, 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n") // "17179869184", "{ 3.40 3.28 3.72 }"
	if len(lines) < 2 || len(strings.Fields(lines[1])) < 2 {
		return 0, 0, 0, fmt.Errorf("unexpected sysctl output: %q", out)
	}
	load, err := strconv.ParseFloat(strings.Fields(lines[1])[1], 64)
	if err != nil {
		return 0, 0, 0, err
	}
	vs, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, 0, 0, err
	}
	var page int // first line: "Mach Virtual Memory Statistics: (page size of 16384 bytes)"
	if i := bytes.Index(vs, []byte("page size of")); i >= 0 {
		fmt.Sscanf(string(vs[i:]), "page size of %d", &page)
	}
	m := kv(vs)
	pages := atoi(m["Pages free"]) + atoi(m["Pages inactive"]) + atoi(m["Pages speculative"])
	return atoi(lines[0]) >> 20, pages * page >> 20, load, nil
}

func init() { register("capacity", collectCapacity) }
