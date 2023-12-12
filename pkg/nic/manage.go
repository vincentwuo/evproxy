package nic

import (
	"errors"
	"net"

	"github.com/vishvananda/netlink"
)

func AddAddr(linkName string, addr string, mask int) error {
	ipToAdd := net.ParseIP(addr)
	if ipToAdd == nil {
		return errors.New("ip not valid")
	}
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return err
	}
	err = netlink.AddrAdd(link, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ipToAdd,
			Mask: net.CIDRMask(mask, 32),
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func DelAddr(linkName string, addr string, mask int) error {
	ipToDel := net.ParseIP(addr)
	if ipToDel == nil {
		return errors.New("ip not valid")
	}
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return err
	}

	err = netlink.AddrDel(link, &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ipToDel,
			Mask: net.CIDRMask(mask, 32),
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func IsAddrOnInterface(linkName, addr string) (bool, error) {
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return false, err
	}
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return false, err
	}
	for _, linkaddr := range addrs {
		if linkaddr.IP.String() == addr {
			return true, nil
		}
	}
	return false, nil
}
