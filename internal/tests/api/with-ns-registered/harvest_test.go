package with_ns_registered

import (
	"testing"

	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/stretchr/testify/require"
)

func TestHarvest(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Make the bindings
	sp := testMgr.GetNode().GetServiceProvider()
	csMgr := sp.GetConstellationManager()
	bindings, err := cstestutils.CreateBindings(mainNode.GetServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	// Make a minipool
	createAndStakeMinipool(t, bindings, mainNode, standardSalt)

	// Fast forward time for reward interval to increment
	slotsToAdvance := 7 * 24 * 60 * 60 / 12 // 7 days
	err = testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	err = testMgr.CommitBlock()
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	// Fund the YieldDistributor
	//fundAmount := big.NewInt(1e18)
	//err = testMgr.Constellation_FundYieldDistributor(bindings.Weth, fundAmount, deployerOpts)
	//require.NoError(t, err)
	//t.Logf("Funded the YieldDistributor with %.6f WETH", eth.WeiToEth(fundAmount))

	cstestutils.HarvestRewards(t, testMgr, mainNode, bindings.Weth, csMgr.Treasury.Address, nodeAddress, deployerOpts)
}
