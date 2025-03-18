package utils

import (
	"context"
	json2 "encoding/json"
	"github.com/bytedance/sonic"
	"github.com/drpcorg/dshaltie/internal/protocol"
	"github.com/stretchr/testify/mock"
)

func GetResultAsBytes(json []byte) []byte {
	var parsed map[string]json2.RawMessage
	err := sonic.Unmarshal(json, &parsed)
	if err != nil {
		panic(err)
	}
	return parsed["result"]
}

type HttpConnectorMock struct {
	mock.Mock
}

func NewHttpConnectorMock() *HttpConnectorMock {
	return &HttpConnectorMock{}
}

func (c *HttpConnectorMock) SendRequest(ctx context.Context, request protocol.UpstreamRequest) protocol.UpstreamResponse {
	args := c.Called(ctx, request)
	return args.Get(0).(protocol.UpstreamResponse)
}

func (c *HttpConnectorMock) Subscribe(ctx context.Context, request protocol.UpstreamRequest) (protocol.UpstreamSubscriptionResponse, error) {
	return nil, nil
}

func (c *HttpConnectorMock) GetType() protocol.ApiConnectorType {
	return protocol.JsonRpcConnector
}
