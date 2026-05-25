package httpapi_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Paths to skip when comparing (not implemented in our app).
var skipPaths = map[string]bool{}

func projectRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// --- Normalised representation shared by both formats ---

type normParam struct {
	Name     string
	Type     string
	In       string // "query" or "formData"
	Required bool
}

type normOperation struct {
	Method         string
	Summary        string
	QueryParams    []normParam
	FormParams     []normParam
	ResponseCodes  []string
	ResponseSchema map[string]string // status-code -> schema name
}

type normSpec struct {
	Ops map[string]normOperation // key = "METHOD /path"
}

// --- Swagger 2.0 parser ---

func parseSwagger2(data []byte) (normSpec, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return normSpec{}, err
	}

	var paths map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw["paths"], &paths); err != nil {
		return normSpec{}, err
	}

	ns := normSpec{Ops: map[string]normOperation{}}
	for path, methods := range paths {
		if skipPaths[path] {
			continue
		}
		for method, opRaw := range methods {
			var op struct {
				Summary    string `json:"summary"`
				Parameters []struct {
					Name     string `json:"name"`
					In       string `json:"in"`
					Type     string `json:"type"`
					Required bool   `json:"required"`
				} `json:"parameters"`
				Responses map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"responses"`
			}
			if err := json.Unmarshal(opRaw, &op); err != nil {
				return normSpec{}, fmt.Errorf("parse %s %s: %w", method, path, err)
			}

			norm := normOperation{
				Method:         strings.ToUpper(method),
				Summary:        op.Summary,
				ResponseSchema: map[string]string{},
			}

			for _, p := range op.Parameters {
				np := normParam{Name: p.Name, Type: p.Type, In: p.In, Required: p.Required}
				switch p.In {
				case "query":
					norm.QueryParams = append(norm.QueryParams, np)
				case "formData":
					norm.FormParams = append(norm.FormParams, np)
				}
			}

			for code, resp := range op.Responses {
				norm.ResponseCodes = append(norm.ResponseCodes, code)
				if resp.Schema.Ref != "" {
					parts := strings.Split(resp.Schema.Ref, "/")
					name := parts[len(parts)-1]
					name = strings.TrimPrefix(name, "httpapi.")
					norm.ResponseSchema[code] = name
				}
			}
			sort.Strings(norm.ResponseCodes)
			sortParams(norm.QueryParams)
			sortParams(norm.FormParams)

			key := fmt.Sprintf("%s %s", norm.Method, path)
			ns.Ops[key] = norm
		}
	}
	return ns, nil
}

// --- OpenAPI 3.1 parser ---

func parseOpenAPI3(data []byte) (normSpec, error) {
	var raw struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return normSpec{}, err
	}

	ns := normSpec{Ops: map[string]normOperation{}}
	for path, methods := range raw.Paths {
		if skipPaths[path] {
			continue
		}
		for method, opRaw := range methods {
			var op struct {
				Summary    string `json:"summary"`
				Parameters []struct {
					Name     string `json:"name"`
					In       string `json:"in"`
					Required bool   `json:"required"`
					Schema   struct {
						Type string `json:"type"`
					} `json:"schema"`
				} `json:"parameters"`
				RequestBody struct {
					Content map[string]struct {
						Schema struct {
							Ref string `json:"$ref"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"requestBody"`
				Responses map[string]struct {
					Content map[string]struct {
						Schema struct {
							Ref string `json:"$ref"`
						} `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			}
			if err := json.Unmarshal(opRaw, &op); err != nil {
				return normSpec{}, fmt.Errorf("parse %s %s: %w", method, path, err)
			}

			norm := normOperation{
				Method:         strings.ToUpper(method),
				Summary:        op.Summary,
				ResponseSchema: map[string]string{},
			}

			for _, p := range op.Parameters {
				t := p.Schema.Type
				if t == "" {
					t = "string"
				}
				np := normParam{Name: p.Name, Type: t, In: p.In, Required: p.Required}
				if p.In == "query" {
					norm.QueryParams = append(norm.QueryParams, np)
				}
			}

			// Extract form params from requestBody -> schema -> components/schemas/Body_*
			for _, ct := range op.RequestBody.Content {
				if ct.Schema.Ref != "" {
					parts := strings.Split(ct.Schema.Ref, "/")
					bodySchemaName := parts[len(parts)-1]
					if schema, ok := raw.Components.Schemas[bodySchemaName]; ok {
						reqSet := map[string]bool{}
						for _, r := range schema.Required {
							reqSet[r] = true
						}
						for fieldName := range schema.Properties {
							norm.FormParams = append(norm.FormParams, normParam{
								Name:     fieldName,
								Type:     "string",
								In:       "formData",
								Required: reqSet[fieldName],
							})
						}
					}
				}
			}

			for code, resp := range op.Responses {
				norm.ResponseCodes = append(norm.ResponseCodes, code)
				for _, ct := range resp.Content {
					if ct.Schema.Ref != "" {
						parts := strings.Split(ct.Schema.Ref, "/")
						norm.ResponseSchema[code] = parts[len(parts)-1]
					}
				}
			}
			sort.Strings(norm.ResponseCodes)
			sortParams(norm.QueryParams)
			sortParams(norm.FormParams)

			key := fmt.Sprintf("%s %s", norm.Method, path)
			ns.Ops[key] = norm
		}
	}
	return ns, nil
}

func sortParams(ps []normParam) {
	sort.Slice(ps, func(i, j int) bool { return ps[i].Name < ps[j].Name })
}

// --- Schemas comparison ---

type normSchema struct {
	Fields []string // sorted field names
}

func extractSwagger2Schemas(data []byte) (map[string]normSchema, error) {
	var raw struct {
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := map[string]normSchema{}
	for name, def := range raw.Definitions {
		clean := strings.TrimPrefix(name, "httpapi.")
		var fields []string
		for f := range def.Properties {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		out[clean] = normSchema{Fields: fields}
	}
	return out, nil
}

func extractOpenAPI3Schemas(data []byte) (map[string]normSchema, error) {
	var raw struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := map[string]normSchema{}
	for name, def := range raw.Components.Schemas {
		if strings.HasPrefix(name, "Body_") || name == "StandardResponse" {
			continue
		}
		var fields []string
		for f := range def.Properties {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		out[name] = normSchema{Fields: fields}
	}
	return out, nil
}

// --- The test ---

func TestSpecMatchesReference(t *testing.T) {
	root := projectRoot()

	swaggerPath := filepath.Join(root, "docs", "swagger.json")
	referencePath := filepath.Join(root, "openapi", "openapi.json")

	swaggerData, err := os.ReadFile(swaggerPath)
	if err != nil {
		t.Fatalf("cannot read swagger.json: %v", err)
	}
	referenceData, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("cannot read openapi.json: %v", err)
	}

	ours, err := parseSwagger2(swaggerData)
	if err != nil {
		t.Fatalf("parse swagger.json: %v", err)
	}
	ref, err := parseOpenAPI3(referenceData)
	if err != nil {
		t.Fatalf("parse openapi.json: %v", err)
	}

	// 1. Check that all reference paths exist in ours
	for key := range ref.Ops {
		if _, ok := ours.Ops[key]; !ok {
			t.Errorf("missing endpoint in our spec: %s", key)
		}
	}
	// Check no extra paths in ours
	for key := range ours.Ops {
		if _, ok := ref.Ops[key]; !ok {
			t.Errorf("extra endpoint in our spec not in reference: %s", key)
		}
	}

	// 2. For each shared endpoint, compare structurally
	for key, refOp := range ref.Ops {
		ourOp, ok := ours.Ops[key]
		if !ok {
			continue
		}

		// Summary
		if ourOp.Summary != refOp.Summary {
			t.Errorf("%s: summary mismatch: ours=%q ref=%q", key, ourOp.Summary, refOp.Summary)
		}

		// Query params
		if len(ourOp.QueryParams) != len(refOp.QueryParams) {
			t.Errorf("%s: query param count mismatch: ours=%d ref=%d", key, len(ourOp.QueryParams), len(refOp.QueryParams))
		} else {
			for i, rp := range refOp.QueryParams {
				op := ourOp.QueryParams[i]
				if op.Name != rp.Name {
					t.Errorf("%s: query param[%d] name mismatch: ours=%q ref=%q", key, i, op.Name, rp.Name)
				}
				if op.Required != rp.Required {
					t.Errorf("%s: query param %q required mismatch: ours=%v ref=%v", key, rp.Name, op.Required, rp.Required)
				}
			}
		}

		// Form params
		if len(ourOp.FormParams) != len(refOp.FormParams) {
			t.Errorf("%s: form param count mismatch: ours=%d ref=%d", key, len(ourOp.FormParams), len(refOp.FormParams))
		} else {
			for i, rp := range refOp.FormParams {
				op := ourOp.FormParams[i]
				if op.Name != rp.Name {
					t.Errorf("%s: form param[%d] name mismatch: ours=%q ref=%q", key, i, op.Name, rp.Name)
				}
				if op.Required != rp.Required {
					t.Errorf("%s: form param %q required mismatch: ours=%v ref=%v", key, rp.Name, op.Required, rp.Required)
				}
			}
		}

		// Response status codes
		if strings.Join(ourOp.ResponseCodes, ",") != strings.Join(refOp.ResponseCodes, ",") {
			t.Errorf("%s: response codes mismatch: ours=%v ref=%v", key, ourOp.ResponseCodes, refOp.ResponseCodes)
		}

		// Response schema names
		for code, refSchema := range refOp.ResponseSchema {
			ourSchema, ok := ourOp.ResponseSchema[code]
			if !ok {
				t.Errorf("%s: missing response schema for status %s", key, code)
				continue
			}
			if ourSchema != refSchema {
				t.Errorf("%s: response %s schema mismatch: ours=%q ref=%q", key, code, ourSchema, refSchema)
			}
		}
	}

	// 3. Compare schemas (field names)
	ourSchemas, err := extractSwagger2Schemas(swaggerData)
	if err != nil {
		t.Fatalf("extract swagger schemas: %v", err)
	}
	refSchemas, err := extractOpenAPI3Schemas(referenceData)
	if err != nil {
		t.Fatalf("extract openapi schemas: %v", err)
	}

	for name, refSchema := range refSchemas {
		ourSchema, ok := ourSchemas[name]
		if !ok {
			t.Errorf("missing schema in our spec: %s", name)
			continue
		}
		refFields := strings.Join(refSchema.Fields, ",")
		ourFields := strings.Join(ourSchema.Fields, ",")
		if ourFields != refFields {
			t.Errorf("schema %s fields mismatch: ours=[%s] ref=[%s]", name, ourFields, refFields)
		}
	}
}
