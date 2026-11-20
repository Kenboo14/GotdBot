package databases

import (
	"fmt"
	"strings"
)

func ComputeAutoDelay(groupCount int) (int, int) {
	switch {
	case groupCount <= 0:
		return 30, 0
	case groupCount < 10:
		return 5, 25
	case groupCount < 30:
		return 10, 20
	case groupCount < 50:
		return 30, 10
	case groupCount < 100:
		return 35, 0
	case groupCount < 200:
		return 45, 0
	default:
		return 60, 0
	}
}

func FormatManualJedaParts(perGroupSec, perAllGroupsMin int) string {
	parts := make([]string, 0, 2)
	if perGroupSec > 0 {
		parts = append(parts, fmt.Sprintf("%d detik · antar-grup", perGroupSec))
	}
	if perAllGroupsMin > 0 {
		switch perAllGroupsMin {
		case 240:
			parts = append(parts, "4 jam · antar-putaran")
		case 360:
			parts = append(parts, "6 jam · antar-putaran")
		default:
			parts = append(parts, fmt.Sprintf("%d menit · antar-putaran", perAllGroupsMin))
		}
	}
	if len(parts) == 0 {
		return "Tanpa jeda"
	}
	return strings.Join(parts, ", ")
}

func (c *JasebConfig) FormatJedaSummary(groupCount int) string {
	if c == nil {
		return "5 detik · antar-grup"
	}
	switch strings.ToLower(strings.TrimSpace(c.DelayMode)) {
	case "aman":
		return "Jeda Aman (200–400 detik · antar-grup)"
	case "otomatis":
		if groupCount > 0 {
			pg, pa := ComputeAutoDelay(groupCount)
			return fmt.Sprintf("Jeda Otomatis (%s)", FormatManualJedaParts(pg, pa))
		}
		return "Jeda Otomatis"
	default:
		return FormatManualJedaParts(c.Delay, c.DelayAllGroups)
	}
}

func (c *JasebConfig) FormatSkipFloodWait() string {
	if c == nil {
		return "Mati"
	}
	if c.SkipFloodWaitSec > 0 {
		if c.SkipFloodWaitSec >= 60 && c.SkipFloodWaitSec%60 == 0 {
			return fmt.Sprintf("Aktif (> %d menit)", c.SkipFloodWaitSec/60)
		}
		return fmt.Sprintf("Aktif (> %d detik)", c.SkipFloodWaitSec)
	}
	if c.SkipFloodWait {
		return "Aktif (Semua)"
	}
	return "Mati"
}

func (c *JasebConfig) FormatTimerDisplay() string {
	if c == nil {
		return "Belum diatur"
	}
	t := strings.TrimSpace(c.Timer)
	if t == "" || strings.EqualFold(t, "Belum diatur") {
		return "Belum diatur"
	}
	if strings.Contains(strings.ToUpper(t), "WIB") {
		return t
	}
	if strings.Contains(t, ":") && strings.Contains(t, "-") {
		return t + " WIB"
	}
	return t
}
