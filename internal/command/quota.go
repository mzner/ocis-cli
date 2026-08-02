package command

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
)

var quotaPattern = regexp.MustCompile(
	`^([0-9]+(?:\.[0-9]+)?)\s*([kmgt]?i?b)?$`,
)

func parseQuota(value string) (*int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "", "default":
		return nil, nil
	case "unlimited":
		total := int64(0)
		return &total, nil
	}
	total, err := parseByteSize(value)
	if err != nil {
		return nil, fmt.Errorf("invalid quota: %w", err)
	}
	return &total, nil
}

func parseByteSize(value string) (int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	matches := quotaPattern.FindStringSubmatch(normalized)
	if matches == nil {
		return 0, fmt.Errorf(
			"invalid byte size %q; use bytes, KB, MB, GB, TB, KiB, MiB, GiB, or TiB",
			value,
		)
	}
	multipliers := map[string]int64{
		"": 1, "b": 1,
		"kb": 1_000, "mb": 1_000_000, "gb": 1_000_000_000,
		"tb":  1_000_000_000_000,
		"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40,
	}
	number, ok := new(big.Rat).SetString(matches[1])
	if !ok {
		return 0, fmt.Errorf("invalid byte-size number %q", matches[1])
	}
	number.Mul(number, new(big.Rat).SetInt64(multipliers[matches[2]]))
	if !number.IsInt() {
		return 0, fmt.Errorf("%q does not resolve to a whole number of bytes", value)
	}
	total := number.Num()
	if !total.IsInt64() || total.Sign() < 0 || total.Cmp(big.NewInt(math.MaxInt64)) > 0 {
		return 0, fmt.Errorf("byte size %q exceeds the supported maximum", value)
	}
	return total.Int64(), nil
}
