package main

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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
	if v := os.Getenv("CAD_SLOTS"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil:
			log.Printf("cad: ignoring invalid CAD_SLOTS=%q", v)
		case n < 0:
			log.Printf("cad: negative CAD_SLOTS=%d clamped to 0", n)
			return 0
		default:
			return n
		}
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
	cpus := runtime.NumCPU()
	if runtime.GOOS == "linux" {
		total, free, load, err = readLinux()
		total, free, cpus = applyCgroup("/sys/fs/cgroup", total, free, cpus)
	} else {
		total, free, load, err = readDarwin()
	}
	if err != nil {
		return nil, err
	}
	p := currentPolicy()
	host, _ := os.Hostname()
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

// applyCgroup narrows host values to cgroup v2 limits under root (missing file or "max" = no limit).
func applyCgroup(root string, total, free, cpus int) (int, int, int) {
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(root, name))
		return strings.TrimSpace(string(b))
	}
	if lim, err := strconv.ParseInt(read("memory.max"), 10, 64); err == nil {
		used, _ := strconv.ParseInt(read("memory.current"), 10, 64)
		if mb := int(lim >> 20); mb < total {
			total = mb
		}
		if mb := int((lim - used) >> 20); mb < free {
			free = mb
		}
		if free < 0 {
			free = 0
		}
	}
	if f := strings.Fields(read("cpu.max")); len(f) == 2 { // "<quota|max> <period>"
		q, e1 := strconv.ParseFloat(f[0], 64)
		p, e2 := strconv.ParseFloat(f[1], 64)
		if e1 == nil && e2 == nil && p > 0 {
			if c := int(math.Ceil(q / p)); c < cpus {
				cpus = c
			}
		}
	}
	return total, free, cpus
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
