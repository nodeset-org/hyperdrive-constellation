package with_ns_registered

import (
	"testing"

	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/stretchr/testify/require"
)

// Run a full cycle test of provisioning RP and Constellation, then depositing and staking multiple minipools on the same node
func TestMultiMinipoolDepositAndStake(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	createAndStakeMinipool(t, bindings, mainNode, standardSalt)
	simulateEthRewardToYieldDistributor(t, bindings, mainNode)
}
