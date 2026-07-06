//go:build linux

package admin

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// systemMemory reads total/available memory from /proc/meminfo.
func systemMemory() map[string]any {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return map[string]any{}
	}
	defer f.Close()
	out := map[string]any{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			out["total_bytes"] = kb * 1024
		case "MemAvailable:":
			out["available_bytes"] = kb * 1024
		}
	}
	return out
}
