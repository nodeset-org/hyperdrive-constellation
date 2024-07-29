package with_ns_registered

import (
	"math/big"
	"testing"

	cstestutils "github.com/nodeset-org/hyperdrive-constellation/internal/tests/utils"
	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/rocket-pool/node-manager-core/eth"
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
	bindings, err := cstestutils.CreateBindings(testMgr.GetConstellationServiceProvider())
	require.NoError(t, err)
	t.Log("Created contract bindings")

	// Make a minipool
	depositAndStakeMinipool(t)

	// Fast forward time for reward interval to increment
	slotsToAdvance := 1200 * 60 * 60 / 12
	err = testMgr.AdvanceSlots(uint(slotsToAdvance), false)
	require.NoError(t, err)
	t.Logf("Advanced %d slots", slotsToAdvance)

	// Fund the YieldDistributor
	fundAmount := big.NewInt(1e18)
	err = testMgr.Constellation_FundYieldDistributor(bindings.Weth, fundAmount, deployerOpts)
	require.NoError(t, err)
	t.Logf("Funded the YieldDistributor with %.6f WETH", eth.WeiToEth(fundAmount))

	cstestutils.HarvestRewards(t, testMgr, bindings.Weth, bindings.TreasuryAddress, nodeAddress, deployerOpts)
}
