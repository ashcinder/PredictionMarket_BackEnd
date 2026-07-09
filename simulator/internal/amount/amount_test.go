package amount

import "testing"

func TestParseBKCToWei(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "whole", raw: "1", want: "1000000000000000000"},
		{name: "fraction", raw: "1.25", want: "1250000000000000000"},
		{name: "leading decimal", raw: ".5", want: "500000000000000000"},
		{name: "smallest unit", raw: "0.000000000000000001", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBKCToWei(tt.raw)
			if err != nil {
				t.Fatalf("ParseBKCToWei(%q) error: %v", tt.raw, err)
			}
			if got.String() != tt.want {
				t.Fatalf("ParseBKCToWei(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseBKCToWeiRejectsInvalid(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "1.0000000000000000001", "abc"} {
		if _, err := ParseBKCToWei(raw); err == nil {
			t.Fatalf("ParseBKCToWei(%q) succeeded, want error", raw)
		}
	}
}
