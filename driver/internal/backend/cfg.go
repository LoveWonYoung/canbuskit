package backend

import "github.com/LoveWonYoung/canbuskit/v2/can"

func normalizeConfig(cfg can.Config) (can.Config, error) {
	return cfg.Normalized()
}
