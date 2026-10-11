//go:build with_quic

package outbound

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/sagernet/sing-box/adapter"
	sbOutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	sbHY2 "github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-quic/hysteria"
	"github.com/sagernet/sing-quic/hysteria2"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

type hysteria2PinnedOptions struct {
	option.Hysteria2OutboundOptions
	TLS *pinnedTLSOptions `json:"tls,omitempty"`
}

func registerHysteria2Pins(registry *sbOutbound.Registry) {
	sbOutbound.Register[hysteria2PinnedOptions](registry, C.TypeHysteria2, newHysteria2PinnedOutbound)
}

func newHysteria2PinnedOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options hysteria2PinnedOptions) (adapter.Outbound, error) {
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, C.ErrTLSRequired
	}
	if options.TLS.UTLS != nil && options.TLS.UTLS.Enabled {
		return nil, fmt.Errorf("Hysteria2 uses QUIC and does not support uTLS; use certificate_sha256 or certificate_public_key_sha256 for server pins")
	}
	if len(options.TLS.CertificateSHA256) == 0 && len(options.TLS.CertificatePublicKeySHA256) == 0 {
		options.Hysteria2OutboundOptions.TLS = &options.TLS.OutboundTLSOptions
		return sbHY2.NewOutbound(ctx, router, logger, tag, options.Hysteria2OutboundOptions)
	}
	tlsConfig, err := newPinnedTLSConfig(ctx, options.Server, *options.TLS)
	if err != nil {
		return nil, err
	}
	var salamanderPassword string
	if options.Obfs != nil {
		if options.Obfs.Password == "" {
			return nil, fmt.Errorf("missing obfs password")
		}
		if options.Obfs.Type != hysteria2.ObfsTypeSalamander {
			return nil, fmt.Errorf("unknown obfs type: %s", options.Obfs.Type)
		}
		salamanderPassword = options.Obfs.Password
	}
	options.UDPFragmentDefault = true
	outboundDialer, err := dialer.New(ctx, options.DialerOptions, options.ServerIsDomain())
	if err != nil {
		return nil, err
	}
	networks := options.Network.Build()
	client, err := hysteria2.NewClient(hysteria2.ClientOptions{
		Context: ctx, Dialer: outboundDialer, Logger: logger,
		BrutalDebug:   options.BrutalDebug,
		ServerAddress: options.ServerOptions.Build(), ServerPorts: options.ServerPorts,
		HopInterval: time.Duration(options.HopInterval),
		SendBPS:     uint64(options.UpMbps * hysteria.MbpsToBps), ReceiveBPS: uint64(options.DownMbps * hysteria.MbpsToBps),
		SalamanderPassword: salamanderPassword, Password: options.Password,
		TLSConfig: tlsConfig, UDPDisabled: !common.Contains(networks, N.NetworkUDP),
	})
	if err != nil {
		return nil, err
	}
	return &pinnedHysteria2Outbound{Adapter: sbOutbound.NewAdapterWithDialerOptions(C.TypeHysteria2, tag, networks, options.DialerOptions), client: client}, nil
}

type pinnedHysteria2Outbound struct {
	sbOutbound.Adapter
	client *hysteria2.Client
}

func (h *pinnedHysteria2Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		return h.client.DialConn(ctx, destination)
	case N.NetworkUDP:
		conn, err := h.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(conn, destination), nil
	default:
		return nil, fmt.Errorf("unsupported network: %s", network)
	}
}

func (h *pinnedHysteria2Outbound) ListenPacket(ctx context.Context, _ M.Socksaddr) (net.PacketConn, error) {
	return h.client.ListenPacket(ctx)
}
func (h *pinnedHysteria2Outbound) InterfaceUpdated() {
	_ = h.client.CloseWithError(fmt.Errorf("network changed"))
}
func (h *pinnedHysteria2Outbound) Close() error { return h.client.CloseWithError(os.ErrClosed) }
