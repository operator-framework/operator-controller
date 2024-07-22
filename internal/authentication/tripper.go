package authentication

import (
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/types"
	utilnet "k8s.io/apimachinery/pkg/util/net"
)

var _ http.RoundTripper = (*TokenInjectingRoundTripper)(nil)

type TokenInjectingRoundTripper struct {
	Tripper     http.RoundTripper
	TokenGetter *TokenGetter
	Key         types.NamespacedName
}

func (tt *TokenInjectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := tt.do(req)
	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		tt.TokenGetter.Delete(tt.Key)
		resp, err = tt.do(req)
	}
	return resp, err
}

func (tt *TokenInjectingRoundTripper) do(req *http.Request) (*http.Response, error) {
	reqClone := utilnet.CloneRequest(req)
	token, err := tt.TokenGetter.Get(reqClone.Context(), tt.Key)
	if err != nil {
		return nil, err
	}

	// Always set the Authorization header to our retrieved token
	reqClone.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	return tt.Tripper.RoundTrip(reqClone)
}
