// Package democases 提供可重复的合成业务案例；所有数据只读，不连接生产系统。
package democases

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

//go:embed cases.json
var source []byte

type Case struct {
	ID          string                      `json:"id"`
	Name        string                      `json:"name"`
	Category    string                      `json:"category"`
	Description string                      `json:"description"`
	Task        string                      `json:"task"`
	Datasets    map[string][]map[string]any `json:"datasets,omitempty"`
}

func List() []Case {
	var cases []Case
	if err := json.Unmarshal(source, &cases); err != nil {
		panic(err)
	}
	for i := range cases {
		cases[i].Datasets = nil
	}
	return cases
}

func Find(id string) (Case, error) {
	var cases []Case
	if err := json.Unmarshal(source, &cases); err != nil {
		return Case{}, err
	}
	for _, c := range cases {
		if c.ID == id {
			return c, nil
		}
	}
	return Case{}, errors.New("案例不存在")
}

var ToolNames = []string{"case_overview", "read_data", "summarize_data"}

func Tools() []any {
	tool := func(name, description string, properties map[string]any, required []string) any {
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, "annotations": map[string]bool{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}}
	}
	str := func(description string) any { return map[string]any{"type": "string", "description": description} }
	return []any{
		tool("case_overview", "查看当前合成演示案例的数据集、字段与任务；不连接真实业务", map[string]any{}, []string{}),
		tool("read_data", "读取固定案例数据，可按包含的文本筛选；禁止任意路径、URL 或 SQL", map[string]any{"dataset": str("case_overview 中的数据集名"), "contains": str("可选，按记录内容匹配，最多100字节"), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 100}}, []string{"dataset"}),
		tool("summarize_data", "按指定文本字段分组，返回记录数与各数值字段之和；金额单位由字段名说明", map[string]any{"dataset": str("数据集名"), "group_by": str("按哪个文本字段分组，例如 channel")}, []string{"dataset", "group_by"}),
	}
}

func decode(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return errors.New("参数格式不正确或包含未支持的字段")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("参数须为一个 JSON 对象")
	}
	return nil
}

func Call(id, tool string, raw json.RawMessage) (any, error) {
	c, err := Find(id)
	if err != nil {
		return nil, err
	}
	if tool == "case_overview" {
		if err := decode(raw, &struct{}{}); err != nil {
			return nil, err
		}
		datasets := map[string]any{}
		for name, rows := range c.Datasets {
			fields := []string{}
			if len(rows) > 0 {
				for key := range rows[0] {
					fields = append(fields, key)
				}
				sort.Strings(fields)
			}
			datasets[name] = map[string]any{"rows": len(rows), "fields": fields}
		}
		return map[string]any{"synthetic": true, "case": c.Name, "task": c.Task, "datasets": datasets}, nil
	}
	if tool == "read_data" {
		var args struct {
			Dataset  string `json:"dataset"`
			Contains string `json:"contains"`
			Limit    *int   `json:"limit"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		rows, ok := c.Datasets[args.Dataset]
		if !ok {
			return nil, errors.New("数据集不存在，请先调用 case_overview")
		}
		limit := 50
		if args.Limit != nil {
			limit = *args.Limit
		}
		if len(args.Contains) > 100 || limit < 1 || limit > 100 {
			return nil, errors.New("筛选最多100字节，读取上限为1至100条")
		}
		matches := []map[string]any{}
		total := 0
		for _, row := range rows {
			encoded, _ := json.Marshal(row)
			if args.Contains != "" && !strings.Contains(strings.ToLower(string(encoded)), strings.ToLower(args.Contains)) {
				continue
			}
			total++
			if len(matches) < limit {
				matches = append(matches, row)
			}
		}
		return map[string]any{"synthetic": true, "dataset": args.Dataset, "matched": total, "records": matches}, nil
	}
	if tool == "summarize_data" {
		var args struct {
			Dataset string `json:"dataset"`
			Group   string `json:"group_by"`
		}
		if err := decode(raw, &args); err != nil {
			return nil, err
		}
		rows, ok := c.Datasets[args.Dataset]
		if !ok {
			return nil, errors.New("数据集不存在")
		}
		type group struct {
			Key  string             `json:"key"`
			Rows int                `json:"rows"`
			Sums map[string]float64 `json:"sums"`
		}
		groups := map[string]*group{}
		for _, row := range rows {
			key, ok := row[args.Group].(string)
			if !ok || args.Group == "" {
				return nil, errors.New("分组字段不存在或不是文本字段")
			}
			g := groups[key]
			if g == nil {
				g = &group{Key: key, Sums: map[string]float64{}}
				groups[key] = g
			}
			g.Rows++
			for field, value := range row {
				if number, ok := value.(float64); ok {
					g.Sums[field] += number
				}
			}
		}
		keys := []string{}
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := []*group{}
		for _, key := range keys {
			out = append(out, groups[key])
		}
		return map[string]any{"synthetic": true, "dataset": args.Dataset, "group_by": args.Group, "groups": out}, nil
	}
	return nil, errors.New("只支持 case_overview、read_data、summarize_data 三个只读工具")
}

// Serve 在连接器内提供标准逐行 JSON-RPC stdio，不打开端口，不接受任意文件或执行命令。
func Serve(id string, input io.Reader, output io.Writer) error {
	if _, err := Find(id); err != nil {
		return err
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "JSON 格式错误"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		response := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		switch req.Method {
		case "initialize":
			response["result"] = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "Ai of Cage 合成业务案例", "version": "1.0.0"}, "instructions": "全部业务数据为合成演示数据。工具只读，请在报告中引用记录 ID。"}
		case "ping":
			response["result"] = map[string]any{}
		case "tools/list":
			response["result"] = map[string]any{"tools": Tools()}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			err := json.Unmarshal(req.Params, &params)
			var value any
			if err == nil {
				value, err = Call(id, params.Name, params.Arguments)
			}
			result := map[string]any{}
			if err != nil {
				result["isError"] = true
				result["content"] = []any{map[string]string{"type": "text", "text": err.Error()}}
			} else {
				data, _ := json.Marshal(value)
				result["content"] = []any{map[string]string{"type": "text", "text": string(data)}}
				result["structuredContent"] = value
			}
			response["result"] = result
		default:
			response["error"] = map[string]any{"code": -32601, "message": "不支持该方法"}
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
