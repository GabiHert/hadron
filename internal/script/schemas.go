package script

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

// discoverItemSchema is one item ops.discover returns: itemSchema plus an optional key (such as PROJ-123).
const discoverItemSchema = `{"type":"object","required":["item_id","title"],"properties":{"item_id":{"type":"string","minLength":1},"title":{"type":"string"},"key":{"type":"string"},"url":{"type":"string"},"fields":{"type":"object"}}}`

const itemSchema = `{"type":"object","required":["item_id","title"],"properties":{"item_id":{"type":"string","minLength":1},"title":{"type":"string"},"url":{"type":"string"},"fields":{"type":"object"}}}`

// DefaultSchemas are the input and output schemas of the standard ops. A manifest's op can
// replace either.
var DefaultSchemas = map[string][2]string{
	"search":          {`{"type":"object","required":["q"],"properties":{"q":{"type":"string"}}}`, `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":` + itemSchema + `}}}`},
	"get":             {`{"type":"object","required":["item_id"],"properties":{"item_id":{"type":"string"}}}`, itemSchema},
	"update":          {`{"type":"object","required":["item_id","field"],"properties":{"item_id":{"type":"string"},"field":{"type":"string"},"value":{},"fields":{"type":"object"}}}`, `{"type":["object","null"],"properties":{"fields":{"type":"object"},"title":{"type":"string"},"url":{"type":"string"}}}`},
	"poll":            {`{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":{"type":"object","required":["item_id"],"properties":{"item_id":{"type":"string"},"fields":{"type":"object"}}}}}}`, `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":` + itemSchema + `}}}`},
	"discover":        {`{"type":"object","properties":{"limit":{"type":"integer"}}}`, `{"type":"object","required":["items"],"properties":{"items":{"type":"array","items":` + discoverItemSchema + `}}}`},
	"sessions.list":   {`{"type":"object"}`, `{"type":"object","required":["sessions"],"properties":{"sessions":{"type":"array","items":{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"status":{"type":"string"},"url":{"type":"string"},"item_id":{"type":"string"}}}}}}`},
	"sessions.output": {`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`, `{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`},
	"sessions.send":   {`{"type":"object","required":["id","text"],"properties":{"id":{"type":"string"},"text":{"type":"string"}}}`, `{"type":["object","null"]}`},
	"sessions.start":  {`{"type":"object","properties":{"item_id":{"type":"string"},"prompt":{"type":"string"}}}`, `{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`},
	"sessions.stop":   {`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`, `{"type":["object","null"]}`},
}

// QuerySchemas are the input and output schemas of every ops.queries.<name>: any args object in,
// groups of counts out, and an optional ttl_s (seconds the result may be served from the cache).
var QuerySchemas = [2]string{`{"type":"object"}`, `{"type":"object","required":["groups"],"properties":{"groups":{"type":"array","items":{"type":"object","required":["key","count"],"properties":{"key":{"type":"string"},"label":{"type":"string"},"count":{"type":"number","minimum":0}}}},"ttl_s":{"type":"number","minimum":1,"maximum":86400}}}`}

// SchemaFor returns the input and output schema of an op: the manifest's, else the default, else
// any object.
func SchemaFor(m Manifest, op string) (in, out json.RawMessage) {
	spec, _ := m.OpSpec(op)
	d, ok := DefaultSchemas[op]
	if strings.HasPrefix(op, "queries.") {
		d, ok = QuerySchemas, true
	}
	if !ok {
		d = [2]string{`{"type":"object"}`, `{}`}
	}
	in, out = json.RawMessage(d[0]), json.RawMessage(d[1])
	if len(spec.Input) > 0 {
		in = spec.Input
	}
	if len(spec.Output) > 0 {
		out = spec.Output
	}
	return in, out
}

var (
	cacheMu sync.Mutex
	cache   = map[string]*jsonschema.Resolved{}
)

func resolve(raw json.RawMessage) (*jsonschema.Resolved, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if rs, ok := cache[string(raw)]; ok {
		return rs, nil
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("the schema is not valid JSON Schema: %w", err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("the schema does not resolve: %w", err)
	}
	if len(cache) > 256 {
		cache = map[string]*jsonschema.Resolved{}
	}
	cache[string(raw)] = rs
	return rs, nil
}

// Validate checks a JSON value (as raw JSON) against a schema and says what is wrong.
func Validate(schema json.RawMessage, value json.RawMessage) error {
	rs, err := resolve(schema)
	if err != nil {
		return err
	}
	var v any
	if len(strings.TrimSpace(string(value))) == 0 {
		value = json.RawMessage("null")
	}
	if err := json.Unmarshal(value, &v); err != nil {
		return err
	}
	if err := rs.Validate(v); err != nil {
		return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), "\n", "; "))
	}
	return nil
}

// ValidationError says an op's input or output broke its schema.
type ValidationError struct {
	Op     string
	Where  string // input or output
	Detail string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("op %s: the %s does not match its schema: %s", e.Op, e.Where, e.Detail)
}

// Host call argument schemas.
var hostSchemas = map[string]json.RawMessage{
	"http.request":        json.RawMessage(`{"type":"object","required":["url"],"properties":{"method":{"enum":["GET","POST","PUT","PATCH","DELETE","HEAD","get","post","put","patch","delete","head"]},"url":{"type":"string","minLength":1},"headers":{"type":"object"},"query":{"type":"object"},"json":{},"body":{"type":"string"},"auth":{"type":"object","required":["type"],"properties":{"type":{"enum":["bearer","basic","header"]},"user":{"type":"string"},"name":{"type":"string"},"prefix":{"type":"string"}}}},"additionalProperties":false}`),
	"mcp.call":            json.RawMessage(`{"type":"array","minItems":2,"maxItems":3,"prefixItems":[{"type":"string","minLength":1},{"type":"string","minLength":1},{"type":["object","null"]}]}`),
	"exec":                json.RawMessage(`{"type":"array","minItems":1,"maxItems":2,"prefixItems":[{"type":"string","minLength":1},{"type":"array","items":{"type":["string","number","boolean"]}}]}`),
	"link.upsert":         json.RawMessage(`{"type":"object","required":["card","item_id"],"properties":{"card":{"type":"string","minLength":1},"integration":{"type":"string"},"item_id":{"type":"string","minLength":1},"title":{"type":"string"},"url":{"type":"string"},"fields":{"type":"object"}},"additionalProperties":false}`),
	"link.list":           json.RawMessage(`{"type":"object","properties":{"card":{"type":"string"},"integration":{"type":"string"}},"additionalProperties":false}`),
	"field.set":           json.RawMessage(`{"type":"object","required":["card","path"],"properties":{"card":{"type":"string","minLength":1},"path":{"type":"string","minLength":1},"value":{}},"additionalProperties":false}`),
	"card.read":           json.RawMessage(`{"type":"object","required":["card"],"properties":{"card":{"type":"string","minLength":1}},"additionalProperties":false}`),
	"card.move":           json.RawMessage(`{"type":"object","required":["card","column"],"properties":{"card":{"type":"string","minLength":1},"column":{"type":"string","minLength":1}},"additionalProperties":false}`),
	"card.create":         json.RawMessage(`{"type":"object","properties":{"nick":{"type":"string"},"key":{"type":"string"},"title":{"type":"string"},"column":{"type":"string"},"item_id":{"type":"string"},"url":{"type":"string"},"fields":{"type":"object"}},"additionalProperties":false}`),
	"card.list":           json.RawMessage(`{"type":"object","properties":{"column":{"type":"string"},"expr":{"type":"string"}},"additionalProperties":false}`),
	"session.list_remote": json.RawMessage(`{"type":"object","properties":{"integration":{"type":"string"}},"additionalProperties":false}`),
	"notify":              json.RawMessage(`{"type":"object","required":["body"],"properties":{"title":{"type":"string"},"body":{"type":"string"}},"additionalProperties":false}`),
}
