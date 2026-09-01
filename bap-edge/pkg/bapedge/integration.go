// Package bapedge exposes the deliberately small cross-component surface used
// by BAP System policy-rollout integration tests. The Edge implementation
// remains protected by Go's internal package boundary.
package bapedge

import edgeinternal "bap-system/bap-edge/internal/bapedge"

type Config = edgeinternal.Config
type Client = edgeinternal.Client
type PolicyStore = edgeinternal.PolicyStore

const EdgeProtocolVersion = edgeinternal.EdgeProtocolVersion

var NewClient = edgeinternal.NewClient
var NewPolicyStore = edgeinternal.NewPolicyStore
var EnsurePolicy = edgeinternal.EnsurePolicy
