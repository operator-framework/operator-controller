# Test Registry

This tool is a bare-bones image registry using the `go-containerregistry` library; it is intended to be used in a test environment only. 

Usage:
```
Usage of registry:
      --registry-address string   The address the registry binds to. (default ":12345")
```

The server key and cert locations should be set under the following environment variables:
```
	REGISTRY_HTTP_TLS_CERTIFICATE
	REGISTRY_HTTP_TLS_KEY
```
