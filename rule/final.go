package rules

import (
	C "github.com/yaling888/quirktiva/constant"
)

type Match struct {
	*Base
	adapter string
}

func (f *Match) RuleType() C.RuleType {
	return C.MATCH
}

func (f *Match) Match(metadata *C.Metadata) bool {
	return true
}

func (f *Match) Adapter() string {
	return f.adapter
}

func (f *Match) Payload() string {
	return ""
}

func (f *Match) ShouldResolveIP() bool {
	return false
}

func NewMatch(adapter string) *Match {
	return &Match{
		Base:    &Base{},
		adapter: adapter,
	}
}

var _ C.Rule = (*Match)(nil)
