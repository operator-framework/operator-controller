package storage

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/nlepage/go-tarfs"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rukpakv1alpha1 "github.com/operator-framework/rukpak/api/v1alpha1"
)

type HTTP struct {
	client      http.Client
	requestOpts []func(*http.Request)
}

type HTTPOption func(*HTTP)

func WithInsecureSkipVerify(v bool) HTTPOption {
	return func(s *HTTP) {
		tr := s.client.Transport.(*http.Transport)
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.InsecureSkipVerify = v
	}
}

func WithRootCAs(rootCAs *x509.CertPool) HTTPOption {
	return func(s *HTTP) {
		tr := s.client.Transport.(*http.Transport)
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.RootCAs = rootCAs
	}
}

func WithBearerToken(token string) HTTPOption {
	return func(s *HTTP) {
		s.requestOpts = append(s.requestOpts, func(request *http.Request) {
			request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		})
	}
}

type HTTPRequestOption func(*http.Request)

func NewHTTP(opts ...HTTPOption) *HTTP {
	s := &HTTP{client: http.Client{
		Timeout:   time.Minute,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}}
	for _, f := range opts {
		f(s)
	}
	return s
}

func (s *HTTP) Load(ctx context.Context, owner client.Object) (fs.FS, error) {
	bundle := owner.(*rukpakv1alpha1.Bundle)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bundle.Status.ContentURL, nil)
	if err != nil {
		return nil, err
	}
	for _, f := range s.requestOpts {
		f(req)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response status %q", resp.Status)
	}
	tarReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	return tarfs.New(tarReader)
}
