package outboundgroup

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/yaling888/quirktiva/adapter"
	"github.com/yaling888/quirktiva/adapter/outbound"
	"github.com/yaling888/quirktiva/common/singledo"
	"github.com/yaling888/quirktiva/component/dialer"
	"github.com/yaling888/quirktiva/component/resolver"
	C "github.com/yaling888/quirktiva/constant"
	"github.com/yaling888/quirktiva/constant/provider"
	"github.com/yaling888/quirktiva/tunnel"
)

var _ C.ProxyAdapter = (*Relay)(nil)

type Relay struct {
	*outbound.Base
	disableDNS bool
	single     *singledo.Single[[]C.Proxy]
	providers  []provider.ProxyProvider
}

// DialContext implements C.ProxyAdapter
func (r *Relay) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.Conn, error) {
	var proxies []C.Proxy
	for _, proxy := range r.proxies(metadata, true) {
		if proxy.Type() != C.Direct {
			proxies = append(proxies, proxy)
		}
	}

	length := len(proxies)

	switch length {
	case 0:
		return outbound.NewDirect().DialContext(ctx, metadata, r.Base.DialOptions(opts...)...)
	case 1:
		return proxies[0].DialContext(ctx, metadata, r.Base.DialOptions(opts...)...)
	}

	timeout := time.Duration(length) * C.DefaultTCPTimeout
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = subCtx

	c, err := r.streamContext(ctx, proxies, r.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, err
	}

	last := proxies[len(proxies)-1]
	c, err = last.StreamConn(c, metadata)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", last.Addr(), err)
	}

	return outbound.NewConn(c, r), nil
}

// ListenPacketContext implements C.ProxyAdapter
func (r *Relay) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	var proxies []C.Proxy
	for _, proxy := range r.proxies(metadata, true) {
		if proxy.Type() != C.Direct {
			proxies = append(proxies, proxy)
		}
	}

	length := len(proxies)

	switch length {
	case 0:
		return outbound.NewDirect().ListenPacketContext(ctx, metadata, r.Base.DialOptions(opts...)...)
	case 1:
		proxy := proxies[0]
		if !proxy.SupportUDP() {
			return nil, fmt.Errorf("%s connect error: proxy [%s] UDP is not supported", proxy.Addr(), proxy.Name())
		}
		return proxy.ListenPacketContext(ctx, metadata, r.Base.DialOptions(opts...)...)
	}

	var (
		firstIndex          = 0
		nextIndex           = 1
		lastUDPOverTCPIndex int
		rawUDPRelay         bool

		first = proxies[firstIndex]
		last  = proxies[length-1]

		c   net.Conn
		cc  net.Conn
		err error
	)

	if !supportPacketConn(last) {
		return nil, fmt.Errorf(
			"%s connect error: proxy [%s] UDP is not supported in relay chains", last.Addr(), last.Name(),
		)
	}

	timeout := time.Duration(length) * C.DefaultTCPTimeout
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx = subCtx

	rawUDPRelay, lastUDPOverTCPIndex = isRawUDPRelay(proxies)

	if first.Type() == C.Socks5 {
		cc1, err1 := dialer.DialContext(ctx, "tcp", first.Addr(), r.Base.DialOptions(opts...)...)
		if err1 != nil {
			return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
		}
		cc = cc1
		tcpKeepAlive(cc)

		var pc net.PacketConn
		pc, err = dialer.ListenPacket(ctx, "udp", "", r.Base.DialOptions(opts...)...)
		c = outbound.WrapConn(pc)
	} else if rawUDPRelay {
		var pc net.PacketConn
		pc, err = dialer.ListenPacket(ctx, "udp", "", r.Base.DialOptions(opts...)...)
		c = outbound.WrapConn(pc)
	} else {
		firstIndex = lastUDPOverTCPIndex
		nextIndex = firstIndex + 1
		first = proxies[firstIndex]
		c, err = r.streamContext(ctx, proxies[:nextIndex], r.Base.DialOptions(opts...)...)
	}

	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
	}

	if nextIndex < length {
		var currentMeta *C.Metadata
		for i, proxy := range proxies[nextIndex:] { // raw udp in loop
			currentMeta, err = addrToMetadata(proxy.Addr())
			if err != nil {
				return nil, err
			}
			currentMeta.NetWork = C.UDP

			if !supportPacketConn(first) {
				return nil, fmt.Errorf(
					"%s connect error: proxy [%s] UDP is not supported in relay chains", first.Addr(), first.Name(),
				)
			}

			err = resolveDNS(currentMeta)
			if err != nil {
				return nil, fmt.Errorf("can't resolve ip: %w", err)
			}

			if cc != nil { // socks5
				c, err = streamSocks5PacketConn(first, cc, c, currentMeta)
				cc = nil
			} else {
				c, err = first.StreamPacketConn(c, currentMeta)
			}

			if err != nil {
				return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
			}

			if proxy.Type() == C.Socks5 {
				endIndex := nextIndex + i + 1
				cc, err = r.streamContext(ctx, proxies[:endIndex], r.Base.DialOptions(opts...)...)
				if err != nil {
					return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
				}
			}

			first = proxy
		}
	}

	if cc != nil {
		c, err = streamSocks5PacketConn(last, cc, c, metadata)
	} else {
		c, err = last.StreamPacketConn(c, metadata)
	}

	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", last.Addr(), err)
	}

	return outbound.NewPacketConn(c.(net.PacketConn), r), nil
}

// SupportUDP implements C.ProxyAdapter
func (r *Relay) SupportUDP() bool {
	proxies := r.rawProxies(true)

	l := len(proxies)

	if l == 0 {
		return true
	}

	last := proxies[l-1]

	return isRawUDP(last) || last.SupportUDP()
}

// DisableDnsResolve implements C.DisableDnsResolve
func (r *Relay) DisableDnsResolve() bool {
	return r.disableDNS
}

// MarshalJSON implements C.ProxyAdapter
func (r *Relay) MarshalJSON() ([]byte, error) {
	var all []string
	for _, proxy := range r.rawProxies(false) {
		all = append(all, proxy.Name())
	}
	return json.Marshal(map[string]any{
		"type": r.Type().String(),
		"all":  all,
	})
}

// Cleanup implements C.ProxyAdapter
func (r *Relay) Cleanup() {
	r.single.Reset()
}

func (r *Relay) rawProxies(touch bool) []C.Proxy {
	elm, _, _ := r.single.Do(func() ([]C.Proxy, error) {
		return getProvidersProxies(r.providers, touch), nil
	})

	return elm
}

func (r *Relay) proxies(metadata *C.Metadata, touch bool) []C.Proxy {
	proxies := r.rawProxies(touch)

	for n, proxy := range proxies {
		subProxy := proxy.Unwrap(metadata)
		for subProxy != nil {
			proxies[n] = subProxy
			subProxy = subProxy.Unwrap(metadata)
		}
	}

	return proxies
}

func (r *Relay) streamContext(ctx context.Context, proxies []C.Proxy, opts ...dialer.Option) (net.Conn, error) {
	first := proxies[0]

	c, err := dialer.DialContext(ctx, "tcp", first.Addr(), opts...)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
	}
	tcpKeepAlive(c)

	if len(proxies) > 1 {
		var currentMeta *C.Metadata
		for _, proxy := range proxies[1:] {
			currentMeta, err = addrToMetadata(proxy.Addr())
			if err != nil {
				return nil, err
			}

			c, err = first.StreamConn(c, currentMeta)
			if err != nil {
				return nil, fmt.Errorf("%s connect error: %w", first.Addr(), err)
			}

			first = proxy
		}
	}

	return c, nil
}

func streamSocks5PacketConn(proxy C.Proxy, cc, c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	pc, err := proxy.(*adapter.Proxy).ProxyAdapter.(*outbound.Socks5).
		StreamSocks5PacketConn(cc, c.(net.PacketConn), metadata)
	return outbound.WrapConn(pc), err
}

func isRawUDPRelay(proxies []C.Proxy) (bool, int) {
	var (
		lastIndex           = len(proxies) - 1
		last                = proxies[lastIndex]
		isLastRawUDP        = isRawUDP(last)
		isUDPOverTCP        = false
		lastUDPOverTCPIndex = -1
	)

	for i := lastIndex; i >= 0; i-- {
		p := proxies[i]

		isUDPOverTCP = isUDPOverTCP || !isRawUDP(p)

		if isLastRawUDP && isUDPOverTCP && lastUDPOverTCPIndex == -1 {
			lastUDPOverTCPIndex = i
		}
	}

	if !isLastRawUDP {
		lastUDPOverTCPIndex = lastIndex
	}

	return !isUDPOverTCP, lastUDPOverTCPIndex
}

func isRawUDP(proxy C.ProxyAdapter) bool {
	tp := proxy.Type()
	if ((tp == C.Shadowsocks || tp == C.ShadowsocksR) && proxy.SupportUDP()) || tp == C.WireGuard || tp == C.Socks5 {
		return true
	}
	return false
}

func supportPacketConn(proxy C.Proxy) bool {
	tp := proxy.Type()
	if (tp == C.Shadowsocks || tp == C.ShadowsocksR) && !proxy.SupportUDP() {
		return false
	}
	return proxy.SupportUDP() || !tunnel.UDPFallbackMatch.Load()
}

func resolveDNS(metadata *C.Metadata) error {
	if metadata.Host == "" || metadata.Resolved() {
		return nil
	}

	rAddrs, err := resolver.LookupIP(context.Background(), metadata.Host)
	if err != nil {
		return err
	}
	metadata.DstIP = rAddrs[0]
	return nil
}

func NewRelay(option *GroupCommonOption, providers []provider.ProxyProvider) *Relay {
	return &Relay{
		Base: outbound.NewBase(outbound.BaseOption{
			Name:        option.Name,
			Type:        C.Relay,
			Interface:   option.Interface,
			RoutingMark: option.RoutingMark,
		}),
		single:     singledo.NewSingle[[]C.Proxy](defaultGetProxiesDuration),
		providers:  providers,
		disableDNS: option.DisableDNS,
	}
}
