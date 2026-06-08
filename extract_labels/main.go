// Command extract-labels statically parses Terraform/OpenTofu HCL and reports
// the labels applied to every stack created via the
// spacelift-solutions/terraform-spacelift-stack module.
//
// It walks a directory tree, finds `module` blocks whose `source` matches the
// target module, and extracts the `labels` attribute. Labels written as a
// literal list are resolved to their string values. Labels built from
// references or function calls (locals, var, concat, ...) cannot be resolved
// without a full Terraform evaluation, so the raw expression text is reported
// instead and the stack is flagged as unresolved.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// StackLabels describes the labels found on a single module invocation.
type StackLabels struct {
	// Module is the local name of the module block, e.g. "my_app" in
	// `module "my_app" { ... }`.
	Module string `json:"module"`
	// File is the path to the .tf file that declares the module block.
	File string `json:"file"`
	// Source is the resolved `source` value of the module block.
	Source string `json:"source"`
	// Labels holds the resolved label strings when `labels` is a literal list.
	Labels []string `json:"labels"`
	// Resolved is false when `labels` is an expression (reference, concat,
	// etc.) that cannot be evaluated statically. In that case RawExpr holds
	// the verbatim expression text from the source file.
	Resolved bool `json:"resolved"`
	// RawExpr is the verbatim source text of the `labels` expression. It is
	// only populated when Resolved is false.
	RawExpr string `json:"raw_expr,omitempty"`
}

func main() {
	dir := flag.String("dir", ".", "directory to scan for .tf files")
	match := flag.String("source-match", "terraform-spacelift-stack",
		"substring matched against a module's source to identify the target module")
	asJSON := flag.Bool("json", false, "emit results as JSON")
	flag.Parse()

	results, err := ScanDir(*dir, *match)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	printText(results, *match)
}

// ScanDir walks dir recursively, parses every .tf file, and returns the labels
// for each module block whose source contains sourceMatch.
func ScanDir(dir, sourceMatch string) ([]StackLabels, error) {
	var out []StackLabels

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip vendored modules and VCS metadata.
			if name := d.Name(); name == ".git" || name == ".terraform" {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".tf" {
			return nil
		}

		found, err := parseFile(path, sourceMatch)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, found...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Module < out[j].Module
	})
	return out, nil
}

// parseFile extracts matching stack labels from a single .tf file.
func parseFile(path, sourceMatch string) ([]StackLabels, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, path)
	if diags.HasErrors() {
		return nil, diags
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		// JSON-syntax (.tf.json) bodies are not native HCL syntax; skip them.
		return nil, nil
	}

	var out []StackLabels
	for _, block := range body.Blocks {
		if block.Type != "module" || len(block.Labels) == 0 {
			continue
		}

		source, ok := stringAttr(block.Body, "source")
		if !ok || !strings.Contains(source, sourceMatch) {
			continue
		}

		stack := StackLabels{
			Module: block.Labels[0],
			File:   path,
			Source: source,
		}

		labelsAttr, ok := block.Body.Attributes["labels"]
		if !ok {
			// Module used without labels; report an empty resolved set.
			stack.Resolved = true
			out = append(out, stack)
			continue
		}

		if labels, ok := literalStringList(labelsAttr.Expr); ok {
			stack.Labels = labels
			stack.Resolved = true
		} else {
			stack.RawExpr = exprText(src, labelsAttr.Expr.Range())
		}
		out = append(out, stack)
	}
	return out, nil
}

// stringAttr resolves a literal string attribute (e.g. `source`) from a body.
func stringAttr(body *hclsyntax.Body, name string) (string, bool) {
	attr, ok := body.Attributes[name]
	if !ok {
		return "", false
	}
	val, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || val.IsNull() || !val.Type().Equals(cty.String) {
		return "", false
	}
	return val.AsString(), true
}

// literalStringList returns the string values of an expression when it is a
// list/tuple of string literals. It returns ok=false for any expression that
// references variables, locals, or functions, since those require a full
// Terraform evaluation context to resolve.
func literalStringList(expr hclsyntax.Expression) ([]string, bool) {
	val, diags := expr.Value(nil)
	if diags.HasErrors() || val.IsNull() {
		return nil, false
	}
	if !val.CanIterateElements() {
		return nil, false
	}

	out := []string{}
	for it := val.ElementIterator(); it.Next(); {
		_, ev := it.Element()
		if !ev.Type().Equals(cty.String) {
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

func printText(results []StackLabels, sourceMatch string) {
	if len(results) == 0 {
		fmt.Printf("No module blocks matching %q found.\n", sourceMatch)
		return
	}

	labelSet := map[string]struct{}{}
	var unresolved int

	for _, r := range results {
		fmt.Printf("module %q (%s)\n", r.Module, r.File)
		fmt.Printf("  source: %s\n", r.Source)
		if r.Resolved {
			if len(r.Labels) == 0 {
				fmt.Println("  labels: (none)")
			} else {
				fmt.Printf("  labels: %s\n", strings.Join(r.Labels, ", "))
			}
			for _, l := range r.Labels {
				labelSet[l] = struct{}{}
			}
		} else {
			unresolved++
			fmt.Printf("  labels: <unresolved expression> %s\n", r.RawExpr)
		}
		fmt.Println()
	}

	unique := make([]string, 0, len(labelSet))
	for l := range labelSet {
		unique = append(unique, l)
	}
	sort.Strings(unique)

	fmt.Printf("Found %d module(s); %d with unresolved label expressions.\n",
		len(results), unresolved)
	fmt.Printf("Unique literal labels across all stacks (%d): %s\n",
		len(unique), strings.Join(unique, ", "))
}
