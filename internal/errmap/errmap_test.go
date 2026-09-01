package errmap

import (
	"errors"
	"testing"

	"github.com/situker/qqmailctl/internal/output"
)

func TestExitCodeTable(t *testing.T) {
	cases := []struct {
		kind Kind
		code int
	}{
		{Usage, output.ExitUsage}, {Config, output.ExitConfig}, {AuthFailed, output.ExitAuth},
		{ServiceNotEnabled, output.ExitServiceNotEnabled}, {Network, output.ExitNetwork},
		{TLS, output.ExitTLS}, {RateLimited, output.ExitRateLimited}, {NotFound, output.ExitNotFound},
		{PolicyDenied, output.ExitPolicyDenied}, {ParseError, output.ExitParse}, {Internal, output.ExitInternal},
	}
	for _, tc := range cases {
		_, got := Details(&Error{Kind: tc.kind, Message: "x", Cause: errors.New("fixture")})
		if got != tc.code {
			t.Errorf("%s: got %d want %d", tc.kind, got, tc.code)
		}
	}
}
