package connectors

import (
	"context"
	"github.com/drpcorg/dshaltie/src/protocol"
)

type ApiConnector interface {
	SendRequest(context.Context, protocol.UpstreamRequest) protocol.UpstreamResponse
	Subscribe(context.Context, protocol.UpstreamRequest) (protocol.UpstreamSubscriptionResponse, error)
	GetType() protocol.ApiConnectorType
}
