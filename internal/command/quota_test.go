package command

import "testing"

func TestParseQuota(t *testing.T) {
	tests := []struct {
		value string
		want  *int64
	}{
		{value: ""},
		{value: "default"},
		{value: "unlimited", want: quotaValue(0)},
		{value: "0", want: quotaValue(0)},
		{value: "1234", want: quotaValue(1234)},
		{value: "500MB", want: quotaValue(500_000_000)},
		{value: "1.5 GB", want: quotaValue(1_500_000_000)},
		{value: "2GiB", want: quotaValue(2 << 30)},
		{value: " 1 TB ", want: quotaValue(1_000_000_000_000)},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := parseQuota(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == nil {
				if got != nil {
					t.Fatalf("got %d, want nil", *got)
				}
				return
			}
			if got == nil || *got != *test.want {
				t.Fatalf("got %v, want %d", got, *test.want)
			}
		})
	}
}

func TestParseQuotaRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{
		"-1", "one GB", "1.2B", "1PB", "9223372036854775808", "NaN",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseQuota(value); err == nil {
				t.Fatalf("accepted invalid quota %q", value)
			}
		})
	}
}

func quotaValue(value int64) *int64 {
	return &value
}
