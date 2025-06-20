package server

import (
	"bytes"
	"context"
	"errors"
	"github.com/drpcorg/dsheltie/internal/upstreams/flow"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"io"
	"sync"
)

var upgrader = websocket.Upgrader{}

func handleWebsocket(reqCtx echo.Context, appCtx *ApplicationContext) {
	chain := reqCtx.Param("chain")
	httpRequest := reqCtx.Request()
	ctx := httpRequest.Context()
	log := zerolog.Ctx(ctx)

	subCtx := flow.NewSubCtx()

	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn, err := upgrader.Upgrade(reqCtx.Response().Writer, reqCtx.Request(), nil)
	if err != nil {
		log.Error().Err(err).Msg("couldn't upgrade http to ws")
		return
	}
	defer func() {
		err = conn.Close()
		if err != nil {
			log.Warn().Err(err).Msg("couldn't close a client websocket connection")
		}
	}()

	var wsLock sync.Mutex
	var wg sync.WaitGroup

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			var closedErr *websocket.CloseError
			if ok := errors.As(err, &closedErr); ok {
				if closedErr.Code == websocket.CloseNormalClosure || closedErr.Code == websocket.CloseNoStatusReceived {
					log.Debug().Msg("closing ws connection")
				} else {
					log.Error().Err(err).Msg("couldn't receive a ws message")
				}
			}
			break
		}

		preRequest := &Request{
			Chain: chain,
		}
		requestHandler, err := NewJsonRpcHandler(preRequest, bytes.NewReader(message), true)
		if err != nil {
			log.Error().Err(err).Msg("couldn't create requestHandler")
			break
		}

		responseWrappers := handleRequest(cancelCtx, requestHandler, appCtx, subCtx)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if cancelCtx.Err() == nil {
				for respWrapper := range responseWrappers {
					writeEvent := func() {
						wsLock.Lock()
						defer wsLock.Unlock()
						writer, err := conn.NextWriter(messageType)
						if err != nil {
							log.Error().Err(err).Msg("couldn't get writer to send a response")
						} else {
							resp := requestHandler.ResponseEncode(respWrapper.Response)
							if _, err = io.Copy(writer, resp.ResponseReader); err != nil {
								log.Error().Err(err).Msg("couldn't copy message")
							}
							if err = writer.Close(); err != nil {
								log.Error().Err(err).Msg("couldn't write message")
							}
						}
					}
					writeEvent()
				}
			}
		}()
	}

	cancel()
	wg.Wait()
}
