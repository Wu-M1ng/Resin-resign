//go:build !with_quic

package outbound

import sbOutbound "github.com/sagernet/sing-box/adapter/outbound"

func registerHysteria2Pins(_ *sbOutbound.Registry) {}
