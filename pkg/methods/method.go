package specs

import (
	"context"
	"errors"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/itchyny/gojq"
	"github.com/rs/zerolog"
	"github.com/samber/lo"
	"strings"
)

type Method struct {
	enabled      bool
	cacheable    bool
	parser       *jqParser
	Subscription *Subscription
	Name         string
}

type jqParser struct {
	returnType ParserReturnType
	query      *gojq.Query
}

func DefaultMethod(name string) *Method {
	return &Method{
		Name:      name,
		enabled:   true,
		cacheable: true,
	}
}

func MethodWithSettings(name string, settings *MethodSettings, tagParser *TagParser) *Method {
	methodData := &MethodData{
		Name:      name,
		Enabled:   lo.ToPtr(true),
		Settings:  settings,
		TagParser: tagParser,
	}

	method, err := fromMethodData(methodData)
	if err != nil {
		return nil
	}
	return method
}

func (m *Method) IsCacheable() bool {
	return m.cacheable
}

func (m *Method) Enabled() bool {
	return m.enabled
}

func fromMethodData(methodData *MethodData) (*Method, error) {
	var parser *jqParser
	if methodData.TagParser != nil {
		jqQuery, err := gojq.Parse(methodData.TagParser.Path)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse a jq path of method %s", methodData.Name)
		}
		parser = &jqParser{
			returnType: methodData.TagParser.ReturnType,
			query:      jqQuery,
		}
	}

	var sub *Subscription
	cacheable := true
	if methodData.Settings != nil {
		if methodData.Settings.Cacheable != nil {
			cacheable = *methodData.Settings.Cacheable
		}
		if methodData.Settings.Subscription != nil {
			sub = methodData.Settings.Subscription
		}
	}

	return &Method{
		enabled:      lo.Ternary(methodData.Enabled == nil, true, *methodData.Enabled),
		cacheable:    cacheable,
		Name:         methodData.Name,
		parser:       parser,
		Subscription: sub,
	}, nil
}

type MethodParam interface {
	param()
}

type BlockNumberParam struct { // hex number or tag
	BlockNumber rpc.BlockNumber
}

func (b *BlockNumberParam) param() {
}

type HashTagParam struct { // hash
	Hash string
}

func (b *HashTagParam) param() {
}

func (m *Method) Parse(ctx context.Context, data any) MethodParam {
	if m.parser == nil {
		return nil
	}
	log := zerolog.Ctx(ctx)
	methodParam, err := m.jqParse(data)
	if err != nil {
		log.Warn().Msgf("couldn't parse tag of method %s, cause - %s", m.Name, err.Error())
		return nil
	}
	switch param := methodParam.(type) {
	case string:
		if m.parser.returnType == BlockNumberType && isHexNumberOrTag(param) {
			var num rpc.BlockNumber
			err = sonic.Unmarshal([]byte(fmt.Sprintf(`"%s"`, param)), &num)
			if err != nil {
				log.Warn().Msgf("couldn't parse tag of method to BlockNumber %s, cause - %s", m.Name, err.Error())
				return nil
			}
			return &BlockNumberParam{BlockNumber: num}
		} else if m.parser.returnType == BlockRefType {
			var blockNumberOrHash rpc.BlockNumberOrHash
			err = sonic.Unmarshal([]byte(fmt.Sprintf(`"%s"`, param)), &blockNumberOrHash)
			if err != nil {
				log.Warn().Msgf("couldn't parse tag of method to BlockNumberOrHash %s, cause - %s", m.Name, err.Error())
				return nil
			}
			if blockNumberOrHash.BlockHash != nil {
				return &HashTagParam{Hash: blockNumberOrHash.BlockHash.String()}
			} else if blockNumberOrHash.BlockNumber != nil {
				return &BlockNumberParam{BlockNumber: *blockNumberOrHash.BlockNumber}
			}
		}
	}

	return nil
}

func (m *Method) jqParse(data any) (any, error) {
	iter := m.parser.query.Run(data)
	for {
		param, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := param.(error); ok {
			if err != nil {
				return nil, err
			}
		} else {
			return param, nil
		}
	}
	return nil, errors.New("no parsed value")
}

func isHexNumberOrTag(param string) bool {
	return strings.HasPrefix(param, "0x") || isBlockTag(param)
}

func isBlockTag(param string) bool {
	switch param {
	case "latest", "earliest", "pending", "finalized", "safe":
		return true
	default:
		return false
	}
}

func IsBlockTagNumber(num rpc.BlockNumber) bool {
	switch num {
	case rpc.SafeBlockNumber, rpc.LatestBlockNumber, rpc.PendingBlockNumber, rpc.FinalizedBlockNumber, rpc.EarliestBlockNumber:
		return true
	default:
		return false
	}
}
