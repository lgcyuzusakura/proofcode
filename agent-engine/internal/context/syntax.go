package context

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// syntaxChunks uses the Go parser only for valid Go. Other languages and
// incomplete files use explicitly labelled byte-exact line/regex chunks.
func syntaxChunks(path, content string, lines int) []Chunk {
	if !strings.HasSuffix(strings.ToLower(path), ".go") {
		return heuristicChunks(path, content, lines)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil || len(f.Decls) == 0 {
		return heuristicChunks(path, content, lines)
	}
	imports := []string{}
	for _, imp := range f.Imports {
		if name, e := strconv.Unquote(imp.Path.Value); e == nil {
			imports = append(imports, name)
		}
	}
	type boundary struct {
		start int
		names []string
		calls []string
		kind  string
	}
	decls := []boundary{}
	for _, d := range f.Decls {
		b := boundary{start: fset.Position(d.Pos()).Offset, kind: "declaration"}
		switch node := d.(type) {
		case *ast.FuncDecl:
			b.kind = "function"
			b.names = []string{node.Name.Name}
			if node.Doc != nil {
				b.start = fset.Position(node.Doc.Pos()).Offset
			}
		case *ast.GenDecl:
			b.kind = node.Tok.String()
			if node.Doc != nil {
				b.start = fset.Position(node.Doc.Pos()).Offset
			}
			for _, spec := range node.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					b.names = append(b.names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						b.names = append(b.names, n.Name)
					}
				}
			}
		}
		refs := map[string]bool{}
		ast.Inspect(d, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					refs[fn.Name] = true
				case *ast.SelectorExpr:
					refs[fn.Sel.Name] = true
				}
			}
			return true
		})
		for name := range refs {
			b.calls = append(b.calls, name)
		}
		sort.Strings(b.calls)
		sort.Strings(b.names)
		decls = append(decls, b)
	}
	chunks := []Chunk{}
	for i, b := range decls {
		start := b.start
		if i == 0 {
			start = 0
		}
		end := len(content)
		if i+1 < len(decls) {
			end = decls[i+1].start
		}
		if start < 0 || end < start || end > len(content) {
			return heuristicChunks(path, content, lines)
		}
		parts := exactChunks(path, content[start:end], lines)
		for _, c := range parts {
			c.StartByte += start
			c.EndByte += start
			c.StartLine = 1 + strings.Count(content[:c.StartByte], "\n")
			c.EndLine = c.StartLine + strings.Count(strings.TrimSuffix(c.Content, "\n"), "\n")
			c.Symbol = strings.Join(b.names, ",")
			c.Metadata = map[string]string{"parser": "go/ast", "parserVersion": IndexVersion, "kind": b.kind, "calls": strings.Join(b.calls, ","), "imports": strings.Join(imports, ","), "relationBasis": "static-call-syntax"}
			chunks = append(chunks, c)
		}
	}
	return chunks
}

func heuristicChunks(path, content string, lines int) []Chunk {
	chunks := exactChunks(path, content, lines)
	for i := range chunks {
		chunks[i].Metadata = map[string]string{"parser": "line-regex", "parserVersion": IndexVersion, "relationBasis": "text-reference-heuristic"}
	}
	return chunks
}

func chunkSymbols(c Chunk) []string {
	if c.Metadata["parser"] == "go/ast" {
		if c.Symbol == "" {
			return nil
		}
		return strings.Split(c.Symbol, ",")
	}
	return definitionNames(c.Content)
}

func chunkReferences(c Chunk) map[string]int {
	if c.Metadata["parser"] == "go/ast" {
		return wordCounts(c.Metadata["calls"] + " " + c.Metadata["imports"])
	}
	return wordCounts(definitionPattern.ReplaceAllString(c.Content, ""))
}
