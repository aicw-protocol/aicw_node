// Package identity provides node identity management for AICW MPC network.
package identity

import (
	mpciumidentity "github.com/fystack/mpcium/pkg/identity"
)

// Compile-time interface conformance checks.
// These assertions ensure DynamicFileStore implements both:
// 1. Original mpcium identity.Store interface (14 methods)
// 2. AICW-FORK extended Store interface (5 additional methods)
var (
	_ mpciumidentity.Store = (*DynamicFileStore)(nil) // Original mpcium interface
	_ Store                = (*DynamicFileStore)(nil) // Extended AICW interface
	_ DynamicStore         = (*DynamicFileStore)(nil) // Full dynamic interface
)
