package main

import (
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func exchange(address, domain string, tp uint16) ([]dns.RR, error) {
	client := dns.Client{}
	query := &dns.Msg{}
	query.SetQuestion(dns.Fqdn(domain), tp)

	r, _, err := client.Exchange(query, address)
	if err != nil {
		return nil, err
	}
	return r.Answer, nil
}

func TestClash_DNS(t *testing.T) {
	basic := `
log-level: silent
dns:
  enable: true
  listen: 0.0.0.0:8553
  nameserver:
    - https://dns.alidns.com/dns-query
`

	err := parseAndApply(basic)
	require.NoError(t, err)
	defer cleanup()

	time.Sleep(waitTime)

	rr, err := exchange("127.0.0.1:8553", "1.1.1.1.nip.io", dns.TypeA)
	assert.NoError(t, err)
	assert.NotEmptyf(t, rr, "record empty")

	if len(rr) == 0 {
		return
	}
	record := rr[0].(*dns.A)
	assert.Equal(t, record.A.String(), "1.1.1.1")

	rr, err = exchange("127.0.0.1:8553", "2606-4700-4700--1111.sslip.io", dns.TypeAAAA)
	assert.NoError(t, err)
	assert.Empty(t, rr)
}

func TestClash_DNSHostAndFakeIP(t *testing.T) {
	basic := `
log-level: silent
hosts:
  foo.quirktiva.dev: 1.1.1.1
dns:
  enable: true
  listen: 0.0.0.0:8553
  ipv6: true
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  fake-ip-filter:
    - .sslip.io
  nameserver:
    - https://dns.alidns.com/dns-query
`

	err := parseAndApply(basic)
	require.NoError(t, err)
	defer cleanup()

	time.Sleep(waitTime)

	type domainPair struct {
		domain string
		ip     string
	}

	list := []domainPair{
		{"foo.org", "198.18.0.3"},
		{"bar.org", "198.18.0.4"},
		{"foo.org", "198.18.0.3"},
		{"foo.quirktiva.dev", "1.1.1.1"},
	}

	for _, pair := range list {
		rr, err := exchange("127.0.0.1:8553", pair.domain, dns.TypeA)
		assert.NoError(t, err)
		assert.NotEmpty(t, rr)

		if len(rr) == 0 {
			continue
		}
		record := rr[0].(*dns.A)
		assert.Equal(t, record.A.String(), pair.ip)
	}

	rr, err := exchange("127.0.0.1:8553", "2606-4700-4700--1111.sslip.io", dns.TypeAAAA)
	assert.NoError(t, err)
	assert.NotEmpty(t, rr)
	if len(rr) == 0 {
		return
	}
	assert.Equal(t, rr[0].(*dns.AAAA).AAAA.String(), "2606:4700:4700::1111")
}
