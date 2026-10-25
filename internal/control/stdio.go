// Package control 提供本机 stdio MCP 协议，不监听网络端口。
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
)

type Handler interface {
	Tools() []any
	Call(context.Context, string, json.RawMessage) (any, error)
	Close()
}

func Serve(ctx context.Context, input io.Reader, output io.Writer, handler Handler) error {
	defer handler.Close()
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
		close(lines)
	}()
	encoder := json.NewEncoder(output)
	initialized := false
	for {
		var line []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case value, ok := <-lines:
			if !ok {
				return <-readErr
			}
			line = value
		}
		var request struct {
			Version string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		parseErr := json.Unmarshal(line, &request)
		if parseErr == nil && len(request.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		rpcError := func(code int, message string) { response["error"] = map[string]any{"code": code, "message": message} }
		switch {
		case parseErr != nil:
			response["id"] = nil
			rpcError(-32700, "JSON 格式错误")
		case request.Version != "2.0" || request.Method == "":
			rpcError(-32600, "请求格式错误")
		case request.Method == "initialize":
			initialized = true
			response["result"] = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "Ai of Cage 本机控制", "version": "1.0.0"}, "instructions": "只管理本控制会话新建的沙箱。任务使用所选供应商并消耗额度；正常退出停止本会话的任务和沙箱，保留项目。"}
		case !initialized:
			rpcError(-32002, "请先初始化 MCP 会话")
		case request.Method == "ping":
			response["result"] = map[string]any{}
		case request.Method == "tools/list":
			response["result"] = map[string]any{"tools": handler.Tools()}
		case request.Method == "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			err := json.Unmarshal(request.Params, &params)
			var value any
			if err == nil && params.Name != "" {
				value, err = handler.Call(ctx, params.Name, params.Arguments)
			} else {
				err = errors.New("工具调用参数无效")
			}
			result := map[string]any{}
			if err != nil {
				result["isError"] = true
				result["content"] = []any{map[string]string{"type": "text", "text": err.Error()}}
			} else {
				raw, _ := json.Marshal(value)
				result["content"] = []any{map[string]string{"type": "text", "text": string(raw)}}
				result["structuredContent"] = value
			}
			response["result"] = result
		default:
			rpcError(-32601, "不支持该方法")
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
}
