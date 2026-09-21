package patches

import (
	"fmt"

	"github.com/dop251/goja"
)

// evalExpression runs a JavaScript expression with `value` bound to the current value at the path and `context`
// exposing vObject, hostObject and path. A result of undefined or null means "remove the field".
func evalExpression(expression string, value any, context map[string]any) (result any, remove bool, err error) {
	vm := goja.New()
	if err := vm.Set("value", value); err != nil {
		return nil, false, err
	}
	if err := vm.Set("context", context); err != nil {
		return nil, false, err
	}

	out, err := vm.RunString(expression)
	if err != nil {
		return nil, false, fmt.Errorf("evaluate expression %q: %w", expression, err)
	}
	if out == nil || goja.IsUndefined(out) || goja.IsNull(out) {
		return nil, true, nil
	}
	return normalize(out.Export()), false, nil
}

// normalize converts goja exports into the types the unstructured converter understands.
func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalize(val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = normalize(t[i])
		}
		return t
	case int:
		return int64(t)
	case int32:
		return int64(t)
	case float32:
		return float64(t)
	default:
		return v
	}
}
