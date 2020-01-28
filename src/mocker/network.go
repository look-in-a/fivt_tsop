package mocker

import (
	"fmt"
	"math/rand"
	"net"

	"github.com/docker/libcontainer/netlink"
	"github.com/milosgajdos83/tenus"
	"github.com/vishvananda/netns"
)

type network struct {
	bridge   tenus.Bridger
	veth     tenus.Vether
	newNetNs netns.NsHandle
	oldNetNs netns.NsHandle
}

func InitNetwork() network {
	oldNS, err := netns.Get()
	wrapError(err)
	newNS, err := netns.New()
	wrapError(err)
	wrapError(netns.Set(oldNS))

	veth, err := tenus.NewVethPair()
	wrapError(err, "fail to create vath pair")
	wrapError(veth.SetLinkUp(), "fail to up veth0 interface")

	bridge, err := tenus.BridgeFromName("bridge0")
	wrapError(err, "fail to open bridge")

	wrapError(bridge.AddSlaveIfc(veth.NetInterface()), "fail to set veth0 to bridge0")
	wrapError(netlink.NetworkSetNsFd(veth.PeerNetInterface(), int(newNS)), "fail to set veth1 to newnetns")
	wrapError(netns.Set(newNS), "failed to set new netns")

	lo, err := net.InterfaceByName("lo")
	wrapError(err, "fail to open lo interface")

	wrapError(netlink.NetworkLinkUp(lo), "fail to up lo interface")

	wrapError(netlink.NetworkSetMacAddress(veth.PeerNetInterface(), randMacAddr()), "failed set mac addr to vath1")

	ip, ipNet, err := net.ParseCIDR("10.0.0." + fmt.Sprint(2+rand.Intn(253)) + "/24")
	wrapError(err, "failed parse ip addr string")

	wrapError(netlink.NetworkLinkAddIp(veth.PeerNetInterface(), ip, ipNet), "failed to set ip addr to veth1")
	wrapError(veth.SetPeerLinkUp(), "failed to up veth1 interface")

	wrapError(netlink.AddDefaultGw("10.0.0.1", veth.PeerNetInterface().Name), "failed to set default gateway")

	return network{
		oldNetNs: oldNS,
		newNetNs: newNS,
		veth:     veth,
		bridge:   bridge,
	}
}

func (n *network) Close() {
	if n.veth != nil {
		wrapError(n.veth.DeletePeerLink(), "failed to delete veth1")
	}
	if int(n.oldNetNs) != 0 {
		wrapError(netns.Set(n.oldNetNs), "failed to set origin namespace")
	}
	wrapError(n.oldNetNs.Close(), "failed to set origin namespace")
}
