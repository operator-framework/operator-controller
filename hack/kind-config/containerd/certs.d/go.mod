module hack-cert.d
// This file is present in the certs.d directory to ensure that
// certs.d/host:port directories are not included in the main go
// module. Go modules are not allowed to contain files with ':'
// in their name.
