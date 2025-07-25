package run_infra_host

import (
	"context"
	"encoding/json"
	"github.com/koobox/unboxed/pkg/types"
	"github.com/koobox/unboxed/pkg/util"
	"log/slog"
	"os"
	"time"
)

func (rn *RunInfraHost) runReadDnsMap(ctx context.Context) {
	util.LoopWithPrintErr(ctx, "runReadDnsMapOnce", 5*time.Second, func() error {
		return rn.runReadDnsMapOnce(ctx)
	})
}

func (rn *RunInfraHost) runReadDnsMapOnce(ctx context.Context) error {
	b, err := os.ReadFile(types.DnsMapFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	h := util.Sha256Sum(b)
	if h == rn.oldDnsMapHash {
		return nil
	}

	var m map[string]string
	err = json.Unmarshal(b, &m)
	if err != nil {
		return err
	}

	slog.InfoContext(ctx, "updating dns proxy static hosts map", slog.Any("dnsMap", m))

	rn.dnsProxy.SetStaticHostsMap(m)

	rn.oldDnsMapHash = h

	return nil
}
