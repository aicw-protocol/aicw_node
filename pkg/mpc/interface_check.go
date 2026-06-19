// Package mpc provides MPC node functionality for AICW network.
package mpc

import (
	mpciumpc "github.com/fystack/mpcium/pkg/mpc"
)

// Compile-time interface conformance checks.
// These assertions ensure DynamicRegistry implements original mpcium PeerRegistry interface.
var (
	_ mpciumpc.PeerRegistry = (*DynamicRegistry)(nil) // Original mpcium interface
	_ DynamicPeerRegistry   = (*DynamicRegistry)(nil) // Extended AICW interface
)
