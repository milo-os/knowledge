package cel

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/ext"
)

var interfaceSliceType = reflect.TypeOf([]interface{}{})

// runtimeCostLimit matches the limit used by the milo quota engine and the
// Kubernetes apiserver CEL cost budget.
const runtimeCostLimit = 1000000

// Engine evaluates RelationshipPolicy CEL expressions and caches compiled
// programs by expression string.
type Engine struct {
	env   *cel.Env
	cache sync.Map // map[string]cel.Program
}

// NewEngine constructs a CEL environment that binds `subject` as a dynamic map
// and `candidates` as a dynamic list, and expects expressions to return a list
// of object reference maps.
func NewEngine() (*Engine, error) {
	env, err := cel.NewEnv(
		cel.Variable("subject", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("candidates", cel.ListType(cel.DynType)),
		ext.Lists(),
		toSetExtension(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL environment: %w", err)
	}
	return &Engine{env: env}, nil
}

// toSetExtension adds a list.toSet() function that removes duplicate elements
// from a list, using string representation for comparison.
func toSetExtension() cel.EnvOption {
	return cel.Function("toSet",
		cel.MemberOverload("list_to_set",
			[]*cel.Type{cel.ListType(cel.DynType)},
			cel.ListType(cel.DynType),
			cel.UnaryBinding(func(arg ref.Val) ref.Val {
				list, ok := arg.(traits.Lister)
				if !ok {
					return types.NewErr("toSet: expected list, got %T", arg)
				}
				seen := map[string]bool{}
				var out []ref.Val
				iter := list.Iterator()
				for iter.HasNext() == types.True {
					item := iter.Next()
					key := fmt.Sprintf("%v", item)
					if !seen[key] {
						seen[key] = true
						out = append(out, item)
					}
				}
				return types.DefaultTypeAdapter.NativeToValue(out)
			}),
		),
	)
}

// Evaluate compiles (with caching) and evaluates expression against subject and candidates.
// candidates is the list of potential object resources (used by expressions that filter via
// label/selector matching); pass nil or empty slice when not needed.
// Expression must return a list of maps; each map represents an object
// reference with keys: apiGroup, kind, name, namespace,
// controlPlaneContextKind, controlPlaneContextName.
func (e *Engine) Evaluate(_ context.Context, expression string, subject map[string]interface{}, candidates []map[string]interface{}) ([]map[string]interface{}, error) {
	program, err := e.getOrCompile(expression)
	if err != nil {
		return nil, err
	}

	candidatesIface := make([]interface{}, len(candidates))
	for i, c := range candidates {
		candidatesIface[i] = c
	}
	out, details, err := program.Eval(map[string]interface{}{
		"subject":    subject,
		"candidates": candidatesIface,
	})
	if err != nil {
		if details != nil && details.ActualCost() != nil {
			return nil, fmt.Errorf("CEL evaluation failed (cost: %d, limit: %d): %w", *details.ActualCost(), runtimeCostLimit, err)
		}
		return nil, fmt.Errorf("CEL evaluation failed: %w", err)
	}

	return toRefList(out)
}

func (e *Engine) getOrCompile(expression string) (cel.Program, error) {
	if cached, ok := e.cache.Load(expression); ok {
		return cached.(cel.Program), nil
	}

	ast, issues := e.env.Parse(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL parse error: %w", issues.Err())
	}
	checked, issues := e.env.Check(ast)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("CEL type check error: %w", issues.Err())
	}

	program, err := e.env.Program(checked,
		cel.EvalOptions(cel.OptOptimize, cel.OptTrackCost),
		cel.CostLimit(runtimeCostLimit),
	)
	if err != nil {
		return nil, fmt.Errorf("CEL program creation failed: %w", err)
	}

	actual, _ := e.cache.LoadOrStore(expression, program)
	return actual.(cel.Program), nil
}

func toRefList(out ref.Val) ([]map[string]interface{}, error) {
	if out == nil {
		return nil, nil
	}
	// Convert to []interface{} via the CEL native conversion.
	raw, err := out.ConvertToNative(interfaceSliceType)
	if err != nil {
		// Empty list is also acceptable; check for "no such overload" by type.
		if out.Type() == types.NullType {
			return nil, nil
		}
		return nil, fmt.Errorf("CEL expression must return a list, got %s: %w", out.Type().TypeName(), err)
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("CEL expression must return a list, got %T", raw)
	}

	refs := make([]map[string]interface{}, 0, len(list))
	for i, item := range list {
		m, err := toStringMap(i, item)
		if err != nil {
			return nil, err
		}
		refs = append(refs, m)
	}
	return refs, nil
}

// toStringMap converts a CEL map value (in any of the forms it may take after
// ConvertToNative) into a map[string]interface{}.
func toStringMap(i int, item interface{}) (map[string]interface{}, error) {
	switch v := item.(type) {
	case map[string]interface{}:
		return v, nil
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(v))
		for k, val := range v {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("CEL list element %d has non-string map key %v", i, k)
			}
			m[ks] = val
		}
		return m, nil
	case map[ref.Val]ref.Val:
		m := make(map[string]interface{}, len(v))
		for k, val := range v {
			ks, ok := k.Value().(string)
			if !ok {
				return nil, fmt.Errorf("CEL list element %d has non-string map key %v", i, k)
			}
			m[ks] = val.Value()
		}
		return m, nil
	default:
		// Try to use the ref.Val interface if the item implements it.
		if rv, ok := item.(ref.Val); ok {
			native, err := rv.ConvertToNative(reflect.TypeOf(map[string]interface{}{}))
			if err == nil {
				if m, ok := native.(map[string]interface{}); ok {
					return m, nil
				}
			}
		}
		return nil, fmt.Errorf("CEL list element %d is not a map (got %T)", i, item)
	}
}
