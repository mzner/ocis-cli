// Package app coordinates authentication, configuration, and WebDAV use cases
// for the ocis executable.
//
// It is internal so consumers cannot accidentally depend on an unstable CLI
// implementation API. Reusable protocol and persistence code belongs in
// sibling internal packages.
package app
