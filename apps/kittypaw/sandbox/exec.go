package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/jinto/gopaw/core"
)

// run executes JS code in an in-process goja VM.
// Skill calls are resolved synchronously: the JS stub calls a Go function
// that invokes the resolver and returns the result directly.
func run(ctx context.Context, cfg core.SandboxConfig, code string, jsContext map[string]any, resolver SkillResolver) (*core.ExecutionResult, error) {
	if jsContext == nil {
		jsContext = map[string]any{}
	}

	vm := goja.New()

	// --- timeout ---
	timeout := time.Duration(cfg.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("execution timed out")
		case <-done:
		}
	}()
	defer close(done)

	result := &core.ExecutionResult{Success: true}

	// --- console.log capture ---
	var consoleLogs []string
	console := vm.NewObject()
	console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		consoleLogs = append(consoleLogs, strings.Join(parts, " "))
		return goja.Undefined()
	})
	vm.Set("console", console)

	// --- skill stubs ---
	for _, skill := range core.SkillRegistry {
		obj := vm.NewObject()
		skillName := skill.Name
		for _, method := range skill.Methods {
			methodName := method.Name
			obj.Set(methodName, func(call goja.FunctionCall) goja.Value {
				rawArgs := make([]json.RawMessage, len(call.Arguments))
				for i, arg := range call.Arguments {
					exported := arg.Export()
					b, err := json.Marshal(exported)
					if err != nil {
						rawArgs[i] = json.RawMessage("null")
					} else {
						rawArgs[i] = b
					}
				}
				sc := core.SkillCall{
					SkillName: skillName,
					Method:    methodName,
					Args:      rawArgs,
				}
				result.SkillCalls = append(result.SkillCalls, sc)

				if resolver != nil {
					resp, err := resolver(ctx, sc)
					if err == nil && resp != "" {
						var parsed any
						if json.Unmarshal([]byte(resp), &parsed) == nil {
							return vm.ToValue(parsed)
						}
					}
				}
				return goja.Null()
			})
		}
		vm.Set(skillName, obj)
	}

	// --- inject context ---
	vm.Set("context", jsContext)

	// --- execute ---
	wrapped := autoReturn(code)
	script := fmt.Sprintf("(function(){\n%s\n})()", wrapped)

	val, err := vm.RunString(script)

	if err != nil {
		// goja wraps JS exceptions in *goja.Exception.
		if ex, ok := err.(*goja.Exception); ok {
			result.Success = false
			result.Error = ex.Value().String()
		} else if err.Error() == "execution timed out" || ctx.Err() != nil {
			result.Success = false
			result.Error = "execution timed out"
		} else {
			result.Success = false
			result.Error = err.Error()
		}
		if len(consoleLogs) > 0 {
			result.Output = strings.Join(consoleLogs, "\n")
		}
		return result, nil
	}

	// --- build output ---
	var jsonResult string
	if val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
		exported := val.Export()
		b, marshalErr := json.Marshal(exported)
		if marshalErr == nil {
			jsonResult = string(b)
		}
	}

	if len(consoleLogs) > 0 && jsonResult != "" {
		result.Output = strings.Join(consoleLogs, "\n") + "\n" + jsonResult
	} else if len(consoleLogs) > 0 {
		result.Output = strings.Join(consoleLogs, "\n")
	} else {
		result.Output = jsonResult
	}

	return result, nil
}
