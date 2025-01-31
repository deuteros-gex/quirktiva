package outboundgroup

import (
	"context"
	"encoding/json"

	"github.com/yaling888/quirktiva/adapter/outbound"
	"github.com/yaling888/quirktiva/common/singledo"
	"github.com/yaling888/quirktiva/component/dialer"
	C "github.com/yaling888/quirktiva/constant"
	"github.com/yaling888/quirktiva/constant/provider"
)

var _ C.ProxyAdapter = (*Fallback)(nil)

type Fallback struct {
	*outbound.Base
	disableUDP bool
	disableDNS bool
	single     *singledo.Single[[]C.Proxy]
	providers  []provider.ProxyProvider
}

func (f *Fallback) Now() string {
	proxy := f.findAliveProxy(false)
	return proxy.Name()
}

// DialContext implements C.ProxyAdapter
func (f *Fallback) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.Conn, error) {
	proxy := f.findAliveProxy(true)
	c, err := proxy.DialContext(ctx, metadata, f.Base.DialOptions(opts...)...)
	if err == nil {
		c.AppendToChains(f)
	}
	return c, err
}

// ListenPacketContext implements C.ProxyAdapter
func (f *Fallback) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (C.PacketConn, error) {
	proxy := f.findAliveProxy(true)
	pc, err := proxy.ListenPacketContext(ctx, metadata, f.Base.DialOptions(opts...)...)
	if err == nil {
		pc.AppendToChains(f)
	}
	return pc, err
}

// SupportUDP implements C.ProxyAdapter
func (f *Fallback) SupportUDP() bool {
	if f.disableUDP {
		return false
	}

	proxy := f.findAliveProxy(false)
	return proxy.SupportUDP()
}

// DisableDnsResolve implements C.DisableDnsResolve
func (f *Fallback) DisableDnsResolve() bool {
	return f.disableDNS
}

// MarshalJSON implements C.ProxyAdapter
func (f *Fallback) MarshalJSON() ([]byte, error) {
	var all []string
	for _, proxy := range f.proxies(false) {
		all = append(all, proxy.Name())
	}
	return json.Marshal(map[string]any{
		"type": f.Type().String(),
		"now":  f.Now(),
		"all":  all,
	})
}

// Unwrap implements C.ProxyAdapter
func (f *Fallback) Unwrap(_ *C.Metadata) C.Proxy {
	proxy := f.findAliveProxy(true)
	return proxy
}

// Cleanup implements C.ProxyAdapter
func (f *Fallback) Cleanup() {
	f.single.Reset()
}

func (f *Fallback) proxies(touch bool) []C.Proxy {
	elm, _, _ := f.single.Do(func() ([]C.Proxy, error) {
		return getProvidersProxies(f.providers, touch), nil
	})

	return elm
}

func (f *Fallback) findAliveProxy(touch bool) C.Proxy {
	proxies := f.proxies(touch)
	for _, proxy := range proxies {
		if proxy.Alive() {
			return proxy
		}
	}

	return proxies[0]
}

func NewFallback(option *GroupCommonOption, providers []provider.ProxyProvider) *Fallback {
	return &Fallback{
		Base: outbound.NewBase(outbound.BaseOption{
			Name:        option.Name,
			Type:        C.Fallback,
			Interface:   option.Interface,
			RoutingMark: option.RoutingMark,
		}),
		single:     singledo.NewSingle[[]C.Proxy](defaultGetProxiesDuration),
		providers:  providers,
		disableUDP: option.DisableUDP,
		disableDNS: option.DisableDNS,
	}
}
