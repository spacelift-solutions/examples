// Command extract-spaces statically derives the set of Spacelift spaces
// required by tenant stacks, by parsing each tenant root's Terraform/OpenTofu
// HCL and resolving the `labels` argument of every component-stack module
// call. Ownership is encoded as `team/<name>` labels; the set of teams on a
// call determines its space:
//
//	one team   -> "<team>"
//	many teams -> "custom-<team1>-<team2>-..." (lowercased, sorted)
//
// Unlike a literal-only parser (see spacelift-solutions/examples/extract_labels,
// which only resolves labels written as literals and leaves a large fraction of
// real-world calls unresolved), this tool builds a static evaluation context
// per tenant root — variable defaults overlaid with tfvars, locals resolved to
// a fixpoint, and the cty stdlib plus file()/jsondecode() rooted at the tenant
// dir — so labels wired through `var.*`, `local.*`, `concat(...)`, or
// ownership-JSON lookups resolve without running Terraform.
//
// Any label expression that still cannot be resolved is a fatal error naming
// the file and expression: unresolved ownership must be fixed at the source,
// never silently skipped.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/tryfunc"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// ModuleCall is one resolved component-stack invocation in a tenant root.
type ModuleCall struct {
	Tenant string   `json:"tenant"`
	Module string   `json:"module"`
	File   string   `json:"file"`
	Labels []string `json:"labels"`
	Teams  []string `json:"teams"`
	Space  string   `json:"space"`
}

// SpaceSpec is the desired state for a single space, aggregated across every
// module call that resolves to it.
type SpaceSpec struct {
	Teams       []string `json:"teams"`
	Description string   `json:"description"`
	Tenants     []string `json:"tenants"`
}

// Output is the desired_spaces.json schema consumed by the spaces-admin
// stack. It deliberately contains no space IDs and no parent space: IDs are
// owned by Terraform state, and the parent arrives via TF_VAR_parent_space_id.
type Output struct {
	GeneratedBy string               `json:"generated_by"`
	Spaces      map[string]SpaceSpec `json:"spaces"`
}

func main() {
	dir := flag.String("dir", ".", "directory whose immediate subdirectories are tenant roots")
	match := flag.String("source-match", "modules/component-stack",
		"substring matched against a module's source to identify component-stack calls")
	teamPrefix := flag.String("team-prefix", "team/", "label prefix identifying an owning team")
	out := flag.String("out", "desired_spaces.json", "output path for the desired spaces JSON (\"-\" for stdout)")
	flag.Parse()

	calls, errs := ScanTenants(*dir, *match, *teamPrefix)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "error:", e)
		}
		fmt.Fprintf(os.Stderr, "\n%d unresolved module call(s). Fix the wiring above; unresolved ownership is never skipped.\n", len(errs))
		os.Exit(1)
	}

	output := Aggregate(calls)

	for _, c := range calls {
		fmt.Printf("tenant %s: module %q (%s) -> space %q (teams: %s)\n",
			c.Tenant, c.Module, c.File, c.Space, strings.Join(c.Teams, ", "))
	}
	fmt.Printf("\n%d space(s) required across %d module call(s).\n", len(output.Spaces), len(calls))

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *out == "-" {
		os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// ScanTenants treats every immediate subdirectory of dir as a tenant root and
// resolves the component-stack module calls in each. It returns all resolved
// calls plus one error string per call (or tenant) that could not be resolved.
func ScanTenants(dir, sourceMatch, teamPrefix string) ([]ModuleCall, []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []string{err.Error()}
	}

	var calls []ModuleCall
	var errs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		tenantCalls, tenantErrs := scanTenant(filepath.Join(dir, entry.Name()), entry.Name(), sourceMatch, teamPrefix)
		calls = append(calls, tenantCalls...)
		errs = append(errs, tenantErrs...)
	}

	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Tenant != calls[j].Tenant {
			return calls[i].Tenant < calls[j].Tenant
		}
		return calls[i].Module < calls[j].Module
	})
	return calls, errs
}

// Aggregate folds resolved module calls into the per-space desired state.
func Aggregate(calls []ModuleCall) Output {
	type acc struct {
		teams   []string
		tenants map[string]struct{}
	}
	spaces := map[string]*acc{}
	for _, c := range calls {
		a, ok := spaces[c.Space]
		if !ok {
			a = &acc{teams: c.Teams, tenants: map[string]struct{}{}}
			spaces[c.Space] = a
		}
		a.tenants[c.Tenant] = struct{}{}
	}

	out := Output{GeneratedBy: "extract-spaces", Spaces: map[string]SpaceSpec{}}
	for name, a := range spaces {
		tenants := make([]string, 0, len(a.tenants))
		for t := range a.tenants {
			tenants = append(tenants, t)
		}
		sort.Strings(tenants)

		var desc string
		if len(a.teams) == 1 {
			desc = fmt.Sprintf("Team space for %s. Used by: %s.", a.teams[0], strings.Join(tenants, ", "))
		} else {
			desc = fmt.Sprintf("Shared space for %s. Used by: %s.", strings.Join(a.teams, "+"), strings.Join(tenants, ", "))
		}
		out.Spaces[name] = SpaceSpec{Teams: a.teams, Description: desc, Tenants: tenants}
	}
	return out
}

// scanTenant builds the tenant's static evaluation context, then resolves the
// labels of every matching module block in its top-level .tf files.
func scanTenant(dir, tenant, sourceMatch, teamPrefix string) ([]ModuleCall, []string) {
	files, diags := parseTenantFiles(dir)
	if diags != nil {
		return nil, []string{fmt.Sprintf("tenant %s: %s", tenant, diags)}
	}

	ctx, ctxErrs := buildContext(dir, files)
	errs := make([]string, 0, len(ctxErrs))
	for _, e := range ctxErrs {
		errs = append(errs, fmt.Sprintf("tenant %s: %s", tenant, e))
	}

	var calls []ModuleCall
	for _, f := range files {
		for _, block := range f.body.Blocks {
			if block.Type != "module" || len(block.Labels) == 0 {
				continue
			}
			source, ok := literalString(block.Body.Attributes["source"])
			if !ok || !strings.Contains(source, sourceMatch) {
				continue
			}

			call, err := resolveCall(block, f, ctx, tenant, teamPrefix)
			if err != "" {
				errs = append(errs, err)
				continue
			}
			calls = append(calls, call)
		}
	}
	return calls, errs
}

// resolveCall evaluates one module block's labels and derives its space.
// A non-empty string return is a fatal, human-readable error.
func resolveCall(block *hclsyntax.Block, f tenantFile, ctx *hcl.EvalContext, tenant, teamPrefix string) (ModuleCall, string) {
	attr, ok := block.Body.Attributes["labels"]
	if !ok {
		return ModuleCall{}, fmt.Sprintf("%s: module %q has no labels argument; ownership labels (%s<name>) are required",
			block.DefRange().String(), block.Labels[0], teamPrefix)
	}

	val, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() || !val.IsWhollyKnown() || val.IsNull() {
		reason := "value is unknown"
		if diags.HasErrors() {
			reason = diags.Errs()[0].Error()
		}
		return ModuleCall{}, fmt.Sprintf("%s: module %q labels could not be resolved statically: %s\n  expression: %s",
			attr.Range().String(), block.Labels[0], reason, exprText(f.src, attr.Expr.Range()))
	}

	labels, ok := stringSlice(val)
	if !ok {
		return ModuleCall{}, fmt.Sprintf("%s: module %q labels resolved to a non-list-of-strings value",
			attr.Range().String(), block.Labels[0])
	}

	teams := teamSet(labels, teamPrefix)
	if len(teams) == 0 {
		return ModuleCall{}, fmt.Sprintf("%s: module %q carries no %s<name> labels; ownership labels are required (labels: %s)",
			attr.Range().String(), block.Labels[0], teamPrefix, strings.Join(labels, ", "))
	}

	return ModuleCall{
		Tenant: tenant,
		Module: block.Labels[0],
		File:   filepath.Base(f.path),
		Labels: labels,
		Teams:  teams,
		Space:  SpaceName(teams),
	}, ""
}

// teamSet extracts the sorted, distinct, lowercased team names from labels.
// It must produce the same set as the component-stack module's HCL:
//
//	sort(distinct([for l in labels : lower(trimprefix(l, "team/")) if startswith(l, "team/")]))
func teamSet(labels []string, prefix string) []string {
	seen := map[string]struct{}{}
	for _, l := range labels {
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		seen[strings.ToLower(strings.TrimPrefix(l, prefix))] = struct{}{}
	}
	teams := make([]string, 0, len(seen))
	for t := range seen {
		teams = append(teams, t)
	}
	sort.Strings(teams)
	return teams
}

// SpaceName derives the space name from a sorted, distinct, lowercased team
// set. It must byte-match the component-stack module's HCL:
//
//	length(teams) == 1 ? teams[0] : "custom-${join("-", teams)}"
func SpaceName(teams []string) string {
	if len(teams) == 1 {
		return teams[0]
	}
	return "custom-" + strings.Join(teams, "-")
}

// tenantFile pairs a parsed .tf body with its source bytes for error excerpts.
type tenantFile struct {
	path string
	src  []byte
	body *hclsyntax.Body
}

// parseTenantFiles parses the top-level .tf files of a tenant root — the same
// set Terraform reads for a root module. Nested directories are intentionally
// not walked.
func parseTenantFiles(dir string) ([]tenantFile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	parser := hclparse.NewParser()
	var files []tenantFile
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		file, diags := parser.ParseHCL(src, path)
		if diags.HasErrors() {
			return nil, diags
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}
		files = append(files, tenantFile{path: path, src: src, body: body})
	}
	return files, nil
}

// buildContext assembles the tenant's static evaluation context: functions,
// var.* (defaults overlaid with tfvars), local.* (fixpoint), and path.*.
// Locals that never resolve are reported as errors only if something needs
// them later — an unresolvable unused local is harmless, so it is skipped
// silently here and surfaces naturally when a labels expression requires it.
func buildContext(dir string, files []tenantFile) (*hcl.EvalContext, []string) {
	funcs := functions(dir)
	baseCtx := &hcl.EvalContext{Functions: funcs}

	vars := map[string]cty.Value{}
	localExprs := map[string]hclsyntax.Expression{}
	var errs []string

	for _, f := range files {
		for _, block := range f.body.Blocks {
			switch block.Type {
			case "variable":
				if len(block.Labels) == 0 {
					continue
				}
				if def, ok := block.Body.Attributes["default"]; ok {
					val, diags := def.Expr.Value(baseCtx)
					if diags.HasErrors() {
						errs = append(errs, fmt.Sprintf("%s: variable %q default: %s",
							def.Range().String(), block.Labels[0], diags.Errs()[0]))
						continue
					}
					vars[block.Labels[0]] = val
				}
			case "locals":
				for name, attr := range block.Body.Attributes {
					localExprs[name] = attr.Expr
				}
			}
		}
	}

	tfvars, tfvarsErrs := loadTfvars(dir, baseCtx)
	errs = append(errs, tfvarsErrs...)
	for name, val := range tfvars {
		vars[name] = val
	}

	ctx := &hcl.EvalContext{
		Functions: funcs,
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(vars),
			"path": cty.ObjectVal(map[string]cty.Value{
				// file() resolves relative paths against the tenant dir, so
				// "${path.root}/x" and plain "x" land in the same place.
				"root":   cty.StringVal("."),
				"module": cty.StringVal("."),
				"cwd":    cty.StringVal("."),
			}),
			"local": cty.EmptyObjectVal,
		},
	}

	// Locals can reference vars and each other in any order; iterate to a
	// fixpoint, resolving whatever each pass makes resolvable.
	resolved := map[string]cty.Value{}
	for {
		progress := false
		for name, expr := range localExprs {
			if _, done := resolved[name]; done {
				continue
			}
			val, diags := expr.Value(ctx)
			if diags.HasErrors() || !val.IsWhollyKnown() {
				continue
			}
			resolved[name] = val
			ctx.Variables["local"] = cty.ObjectVal(resolved)
			progress = true
		}
		if !progress {
			break
		}
	}

	return ctx, errs
}

// loadTfvars reads terraform.tfvars and *.auto.tfvars (sorted, matching
// Terraform's precedence order) from the tenant dir.
func loadTfvars(dir string, ctx *hcl.EvalContext) (map[string]cty.Value, []string) {
	var paths []string
	if p := filepath.Join(dir, "terraform.tfvars"); fileExists(p) {
		paths = append(paths, p)
	}
	auto, _ := filepath.Glob(filepath.Join(dir, "*.auto.tfvars"))
	sort.Strings(auto)
	paths = append(paths, auto...)

	out := map[string]cty.Value{}
	var errs []string
	parser := hclparse.NewParser()
	for _, path := range paths {
		file, diags := parser.ParseHCLFile(path)
		if diags.HasErrors() {
			errs = append(errs, diags.Error())
			continue
		}
		attrs, diags := file.Body.JustAttributes()
		if diags.HasErrors() {
			errs = append(errs, diags.Error())
			continue
		}
		for name, attr := range attrs {
			val, diags := attr.Expr.Value(ctx)
			if diags.HasErrors() {
				errs = append(errs, fmt.Sprintf("%s: %s", path, diags.Errs()[0]))
				continue
			}
			out[name] = val
		}
	}
	return out, errs
}

// functions returns the evaluation functions available to tenant HCL: the
// cty stdlib subset Terraform exposes under the same names, try/can, and
// filesystem functions rooted at the tenant dir.
func functions(baseDir string) map[string]function.Function {
	return map[string]function.Function{
		"can":        tryfunc.CanFunc,
		"coalesce":   stdlib.CoalesceFunc,
		"compact":    stdlib.CompactFunc,
		"concat":     stdlib.ConcatFunc,
		"contains":   stdlib.ContainsFunc,
		"distinct":   stdlib.DistinctFunc,
		"element":    stdlib.ElementFunc,
		"endswith":   suffixFunc(strings.HasSuffix),
		"file":       fileFunc(baseDir),
		"fileexists": fileExistsFunc(baseDir),
		"flatten":    stdlib.FlattenFunc,
		"format":     stdlib.FormatFunc,
		"join":       stdlib.JoinFunc,
		"jsondecode": stdlib.JSONDecodeFunc,
		"jsonencode": stdlib.JSONEncodeFunc,
		"keys":       stdlib.KeysFunc,
		"length":     stdlib.LengthFunc,
		"lookup":     stdlib.LookupFunc,
		"lower":      stdlib.LowerFunc,
		"merge":      stdlib.MergeFunc,
		"replace":    stdlib.ReplaceFunc,
		"sort":       stdlib.SortFunc,
		"split":      stdlib.SplitFunc,
		"startswith": suffixFunc(strings.HasPrefix),
		"title":      stdlib.TitleFunc,
		"trimprefix": stdlib.TrimPrefixFunc,
		"trimspace":  stdlib.TrimSpaceFunc,
		"trimsuffix": stdlib.TrimSuffixFunc,
		"try":        tryfunc.TryFunc,
		"upper":      stdlib.UpperFunc,
		"values":     stdlib.ValuesFunc,
	}
}

// suffixFunc adapts a (string, string) -> bool predicate into a cty function,
// covering startswith/endswith which Terraform defines outside the cty stdlib.
func suffixFunc(pred func(s, affix string) bool) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: "str", Type: cty.String},
			{Name: "affix", Type: cty.String},
		},
		Type: function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.BoolVal(pred(args[0].AsString(), args[1].AsString())), nil
		},
	})
}

// fileFunc mirrors Terraform's file(), resolving relative paths against the
// tenant root so ownership-JSON lookups like
// jsondecode(file("${path.root}/github-tech-org-owners.custom.spacelift.json"))
// behave exactly as they do under terraform plan.
func fileFunc(baseDir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "path", Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			data, err := os.ReadFile(resolvePath(baseDir, args[0].AsString()))
			if err != nil {
				return cty.NilVal, err
			}
			return cty.StringVal(string(data)), nil
		},
	})
}

func fileExistsFunc(baseDir string) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: "path", Type: cty.String}},
		Type:   function.StaticReturnType(cty.Bool),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			return cty.BoolVal(fileExists(resolvePath(baseDir, args[0].AsString()))), nil
		},
	})
}

func resolvePath(baseDir, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// literalString resolves an attribute that must be a literal string, e.g. a
// module block's source.
func literalString(attr *hclsyntax.Attribute) (string, bool) {
	if attr == nil {
		return "", false
	}
	val, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || val.IsNull() || !val.Type().Equals(cty.String) {
		return "", false
	}
	return val.AsString(), true
}

// stringSlice converts a resolved cty list/tuple/set of strings to []string.
func stringSlice(val cty.Value) ([]string, bool) {
	if val.IsNull() || !val.CanIterateElements() {
		return nil, false
	}
	out := []string{}
	for it := val.ElementIterator(); it.Next(); {
		_, ev := it.Element()
		if ev.IsNull() || !ev.Type().Equals(cty.String) {
			return nil, false
		}
		out = append(out, ev.AsString())
	}
	return out, true
}

// exprText returns the verbatim source text covered by an expression's range.
func exprText(src []byte, rng hcl.Range) string {
	if rng.Start.Byte < 0 || rng.End.Byte > len(src) || rng.Start.Byte > rng.End.Byte {
		return ""
	}
	return string(src[rng.Start.Byte:rng.End.Byte])
}
