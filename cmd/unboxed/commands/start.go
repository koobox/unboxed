package commands

import (
	"context"
	"github.com/koobox/unboxed/cmd/unboxed/flags"
	"github.com/koobox/unboxed/pkg/start-box"
	"log/slog"
	"time"
)

type StartCmd struct {
	flags.BoxUrlFlags

	BoxName     string `help:"Specify the box name" required:""`
	VethCidrArg string `help:"CIDR to use for veth pairs. Unboxed will dynamically allocate 2 IPs from this CIDR per box" default:"1.2.3.0/24"`

	WaitBeforeExit *time.Duration `help:"Wait before finally exiting. This gives the process time to print stdout/stderr messages that might be lost. Especially useful in combination with --debug"`
}

func (cmd *StartCmd) Run(g *flags.GlobalFlags) error {
	defer func() {
		if cmd.WaitBeforeExit != nil {
			slog.Info("sleeping before exit")
			time.Sleep(*cmd.WaitBeforeExit)
		}
	}()

	url, err := cmd.GetBoxUrl()
	if err != nil {
		return err
	}

	startBox := start_box.StartBox{
		Debug:           g.Debug,
		BoxUrl:          url,
		BoxName:         cmd.BoxName,
		WorkDir:         g.WorkDir,
		VethNetworkCidr: cmd.VethCidrArg,
	}

	ctx := context.Background()
	err = startBox.Start(ctx)
	if err != nil {
		return err
	}

	return nil
}
