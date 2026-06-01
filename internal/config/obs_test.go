package config_test

import (
	"testing"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
)

func TestObsEndpointIsEnabled(t *testing.T) {
	t.Parallel()

	enabled := true
	disabled := false

	cases := []struct {
		name string
		in   *bool
		want bool
	}{
		{name: "absent block defaults to on", in: nil, want: true},
		{name: "explicit true", in: &enabled, want: true},
		{name: "explicit false", in: &disabled, want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ep := config.ObsEndpoint{Enabled: testCase.in, Addr: ":9090"}
			if got := ep.IsEnabled(); got != testCase.want {
				t.Fatalf("IsEnabled() = %v, want %v", got, testCase.want)
			}
		})
	}
}
