package cscommon

import (
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
)

// =================
// === Requests  ===
// =================

// =================
// === Responses ===
// =================

// ==============
// === Client ===
// ==============

// Client for interacting with the Nodeset server
type NodesetClient struct {
	cs  *ConstellationServiceProvider
	res *csconfig.ConstellationResources
}

// Creates a new Nodeset client
func NewNodesetClient(cs *ConstellationServiceProvider) *NodesetClient {
	return &NodesetClient{
		cs:  cs,
		res: cs.GetResources(),
	}
}
