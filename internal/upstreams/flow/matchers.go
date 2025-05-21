package flow

import (
	"fmt"
	"github.com/drpcorg/dsheltie/internal/protocol"
	"github.com/rs/zerolog/log"
)

type MatchResponseType int

const (
	MethodType MatchResponseType = iota
	AvailabilityType
	SuccessType
)

type MatchResponse interface {
	Cause() string
	Type() MatchResponseType
}

type SuccessResponse struct {
}

func (s SuccessResponse) Type() MatchResponseType {
	return SuccessType
}

func (s SuccessResponse) Cause() string {
	return ""
}

var _ MatchResponse = (*SuccessResponse)(nil)

type MethodResponse struct {
	method string
}

func (m MethodResponse) Type() MatchResponseType {
	return MethodType
}

func (m MethodResponse) Cause() string {
	return fmt.Sprintf("method %s is not supported", m.method)
}

var _ MatchResponse = (*MethodResponse)(nil)

type AvailabilityResponse struct {
}

func (a AvailabilityResponse) Type() MatchResponseType {
	return AvailabilityType
}

func (a AvailabilityResponse) Cause() string {
	return "upstream is not available"
}

var _ MatchResponse = (*AvailabilityResponse)(nil)

type Matcher interface {
	Match(string, *protocol.UpstreamState) MatchResponse
}

type StatusMatcher struct{}

func NewStatusMatcher() *StatusMatcher {
	return &StatusMatcher{}
}

func (s *StatusMatcher) Match(_ string, state *protocol.UpstreamState) MatchResponse {
	if state.Status == protocol.Available {
		return SuccessResponse{}
	} else {
		return AvailabilityResponse{}
	}
}

var _ Matcher = (*StatusMatcher)(nil)

type MethodMatcher struct {
	method string
}

func (m *MethodMatcher) Match(upId string, state *protocol.UpstreamState) MatchResponse {
	if state.UpstreamMethods.HasMethod(m.method) {
		return SuccessResponse{}
	} else {
		return MethodResponse{method: m.method}
	}
}

func NewMethodMatcher(method string) *MethodMatcher {
	return &MethodMatcher{method: method}
}

var _ Matcher = (*MethodMatcher)(nil)

type MultiMatcher struct {
	matchers []Matcher
}

func (m *MultiMatcher) Match(upId string, state *protocol.UpstreamState) MatchResponse {
	var response MatchResponse = SuccessResponse{}
	for _, matcher := range m.matchers {
		matchedResponse := matcher.Match(upId, state)
		if matchedResponse.Type() != SuccessType {
			log.Debug().Msgf("upstream %s check: %s", upId, matchedResponse.Cause())
		}
		if matchedResponse.Type() < response.Type() {
			response = matchedResponse
		}
	}
	return response
}

func NewMultiMatcher(matchers ...Matcher) *MultiMatcher {
	return &MultiMatcher{matchers: matchers}
}
