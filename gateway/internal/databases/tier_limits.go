package databases

import "strings"

type TierPolicy struct {
	Tier                 string
	MaxGroups            int
	MaxLists             int
	DailySendLimit       int
	AllowManualJeda      bool
	AllowSkipFloodWait   bool
	AllowTopic           bool
	AllowExpertExtra     bool
	AllowLicenseToken    bool
	AllowEmailBackup     bool
	AllowCustomWatermark bool
	AllowForwardMode     bool
	AllowDataBackup      bool
}

func NormalizeTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "vip":
		return "Vip"
	case "pro++", "proplus", "pro+":
		return "Pro++"
	case "pro":
		return "Pro"
	default:
		return "Free"
	}
}

func TierRank(tier string) int {
	switch NormalizeTier(tier) {
	case "Vip":
		return 4
	case "Pro++":
		return 3
	case "Pro":
		return 2
	default:
		return 1
	}
}

func GetTierPolicy(tier string) TierPolicy {
	norm := NormalizeTier(tier)
	switch norm {
	case "Vip":
		return TierPolicy{
			Tier:                 "Vip",
			AllowManualJeda:      true,
			AllowSkipFloodWait:   true,
			AllowTopic:           true,
			AllowExpertExtra:     true,
			AllowLicenseToken:    true,
			AllowEmailBackup:     true,
			AllowCustomWatermark: true,
			AllowForwardMode:     true,
			AllowDataBackup:      true,
		}
	case "Pro++":
		return TierPolicy{
			Tier:                 "Pro++",
			AllowManualJeda:      true,
			AllowSkipFloodWait:   true,
			AllowTopic:           true,
			AllowExpertExtra:     true,
			AllowLicenseToken:    true,
			AllowEmailBackup:     true,
			AllowCustomWatermark: true,
			AllowForwardMode:     true,
			AllowDataBackup:      true,
		}
	case "Pro":
		return TierPolicy{
			Tier:               "Pro",
			AllowManualJeda:    true,
			AllowSkipFloodWait: true,
			AllowTopic:         true,
			AllowExpertExtra:   true,
			AllowLicenseToken:  true,
			AllowEmailBackup:   true,
			AllowDataBackup:    true,
		}
	default:
		return TierPolicy{
			Tier:            "Free",
			MaxGroups:       30,
			MaxLists:        1,
			DailySendLimit:  50,
			AllowDataBackup: true,
		}
	}
}

func IsBetterActiveLicense(candidate, current *UserLicense) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	cr := TierRank(candidate.Tier)
	br := TierRank(current.Tier)
	if cr != br {
		return cr > br
	}
	return candidate.ExpiresAt.After(current.ExpiresAt)
}
