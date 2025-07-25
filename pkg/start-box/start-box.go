package start_box

import (
	"context"
	"fmt"
	"github.com/gofrs/flock"
	"github.com/koobox/unboxed/pkg/sandbox"
	"github.com/koobox/unboxed/pkg/selfupdate"
	"github.com/koobox/unboxed/pkg/types"
	"github.com/koobox/unboxed/pkg/util"
	"github.com/rootless-containers/rootlesskit/pkg/parent/cgrouputil"
	"go4.org/netipx"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
)

type StartBox struct {
	Debug bool

	BoxUrl          *url.URL
	BoxName         string
	WorkDir         string
	VethNetworkCidr string

	acquiredVethNetworkCidr *net.IPNet

	boxSpec *types.BoxSpec

	sandbox *sandbox.Sandbox
}

func (rn *StartBox) Start(ctx context.Context) error {
	// Lock the OS Thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if os.Getpid() == 1 {
		slog.InfoContext(ctx, "evacuating cgroup2")
		err := cgrouputil.EvacuateCgroup2("init")
		if err != nil {
			return fmt.Errorf("failed to evacuate root cgroup: %w", err)
		}
	}

	var err error
	rn.boxSpec, err = rn.retrieveBoxSpec(ctx)
	if err != nil {
		return err
	}

	err = os.MkdirAll(rn.WorkDir, 0700)
	if err != nil {
		return err
	}
	err = os.MkdirAll(filepath.Join(rn.WorkDir, "boxes", rn.BoxName), 0700)
	if err != nil {
		return err
	}

	err = selfupdate.SelfUpdateIfNeeded(ctx, rn.boxSpec.UnboxedBinaryUrl, rn.boxSpec.UnboxedBinaryHash, rn.WorkDir)
	if err != nil {
		return err
	}

	err = rn.reserveVethCIDR(ctx)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "using veth cidr", slog.Any("cidr", rn.acquiredVethNetworkCidr.String()))

	rn.loadModules(ctx)

	rn.sandbox = &sandbox.Sandbox{
		Debug:           rn.Debug,
		HostWorkDir:     rn.WorkDir,
		SandboxName:     rn.BoxName,
		SandboxDir:      filepath.Join(rn.WorkDir, "boxes", rn.BoxName),
		BoxSpec:         rn.boxSpec,
		VethNetworkCidr: rn.acquiredVethNetworkCidr,
	}
	err = rn.sandbox.Start(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (rn *StartBox) reserveVethCIDR(ctx context.Context) error {
	slog.InfoContext(ctx, "reserving CIDR for veth pair")

	fl := flock.New(filepath.Join(rn.WorkDir, "veth-cidrs.lock"))
	err := fl.Lock()
	if err != nil {
		return err
	}
	defer fl.Unlock()

	p, err := rn.readVethCidr(rn.BoxName)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else {
		_, rn.acquiredVethNetworkCidr, _ = net.ParseCIDR(p.String())
		return nil
	}

	otherIpsNets, err := rn.readReservedIPs()
	if err != nil {
		return err
	}

	pc, err := netip.ParsePrefix(rn.VethNetworkCidr)
	if err != nil {
		return err
	}
	pr := netipx.RangeOfPrefix(pc)
	if !pr.IsValid() {
		return fmt.Errorf("invalid cidr")
	}

	var b netipx.IPSetBuilder
	b.AddRange(pr)
	for _, op := range otherIpsNets {
		b.RemovePrefix(op)
	}

	ips, err := b.IPSet()
	if err != nil {
		return err
	}
	newPrefix, _, ok := ips.RemoveFreePrefix(30)
	if !ok {
		return fmt.Errorf("failed to reserve veth pair CIDR")
	}

	err = rn.writeVethCidr(&newPrefix)
	if err != nil {
		return err
	}

	_, rn.acquiredVethNetworkCidr, _ = net.ParseCIDR(newPrefix.String())

	return nil
}

func (rn *StartBox) readVethCidr(boxName string) (*netip.Prefix, error) {
	pth := filepath.Join(rn.WorkDir, "boxes", boxName, types.VethIPStoreFile)
	ipB, err := os.ReadFile(pth)
	if err != nil {
		return nil, err
	}
	p, err := netip.ParsePrefix(string(ipB))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (rn *StartBox) writeVethCidr(p *netip.Prefix) error {
	pth := filepath.Join(rn.WorkDir, "boxes", rn.BoxName, types.VethIPStoreFile)
	return util.AtomicWriteFile(pth, []byte(p.String()), 0644)
}

func (rn *StartBox) readReservedIPs() ([]netip.Prefix, error) {
	boxesDir := filepath.Join(rn.WorkDir, "boxes")
	des, err := os.ReadDir(boxesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
	}
	var ret []netip.Prefix
	for _, de := range des {
		if !de.IsDir() {
			continue
		}
		p, err := rn.readVethCidr(de.Name())
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			ret = append(ret, *p)
		}
	}
	return ret, nil
}
