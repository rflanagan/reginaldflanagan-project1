// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Extract example functions from file ASTs.

package doc

import (
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Example struct {
	Name     string // name of the item being exemplified
	Doc      string // example function doc string
	Code     ast.Node
	Play     *ast.File // a whole program version of the example
	Comments []*ast.CommentGroup
	Output   string // expected output
}

func Examples(files ...*ast.File) []*Example {
	var list []*Example
	for _, file := range files {
		hasTests := false // file contains tests or benchmarks
		numDecl := 0      // number of non-import declarations in the file
		var flist []*Example
		for _, decl := range file.Decls {
			if g, ok := decl.(*ast.GenDecl); ok && g.Tok != token.IMPORT {
				numDecl++
				continue
			}
			f, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			numDecl++
			name := f.Name.Name
			if isTest(name, "Test") || isTest(name, "Benchmark") {
				hasTests = true
				continue
			}
			if !isTest(name, "Example") {
				continue
			}
			var doc string
			if f.Doc != nil {
				doc = f.Doc.Text()
			}
			flist = append(flist, &Example{
				Name:     name[len("Example"):],
				Doc:      doc,
				Code:     f.Body,
				Play:     playExample(file, f.Body),
				Comments: file.Comments,
				Output:   exampleOutput(f.Body, file.Comments),
			})
		}
		if !hasTests && numDecl > 1 && len(flist) == 1 {
			// If this file only has one example function, some
			// other top-level declarations, and no tests or
			// benchmarks, use the whole file as the example.
			flist[0].Code = file
			flist[0].Play = playExampleFile(file)
		}
		list = append(list, flist...)
	}
	sort.Sort(exampleByName(list))
	return list
}

var outputPrefix = regexp.MustCompile(`(?i)^[[:space:]]*output:`)

func exampleOutput(b *ast.BlockStmt, comments []*ast.CommentGroup) string {
	if _, last := lastComment(b, comments); last != nil {
		// test that it begins with the correct prefix
		text := last.Text()
		if loc := outputPrefix.FindStringIndex(text); loc != nil {
			return strings.TrimSpace(text[loc[1]:])
		}
	}
	return "" // no suitable comment found
}

// isTest tells whether name looks like a test, example, or benchmark.
// It is a Test (say) if there is a character after Test that is not a
// lower-case letter. (We don't want Testiness.)
func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) { // "Test" is ok
		return true
	}
	rune, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(rune)
}

type exampleByName []*Example

func (s exampleByName) Len() int           { return len(s) }
func (s exampleByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s exampleByName) Less(i, j int) bool { return s[i].Name < s[j].Name }

// playExample synthesizes a new *ast.File based on the provided
// file with the provided function body as the body of main.
func playExample(file *ast.File, body *ast.BlockStmt) *ast.File {
	if !strings.HasSuffix(file.Name.Name, "_test") {
		// We don't support examples that are part of the
		// greater package (yet).
		return nil
	}

	// Find unresolved identifiers 
	unresolved := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		// For an expression like fmt.Println, only add "fmt" to the
		// set of unresolved names.
		if e, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := e.X.(*ast.Ident); ok && id.Obj == nil {
				unresolved[id.Name] = true
			}
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Obj == nil {
			unresolved[id.Name] = true
		}
		return true
	})

	// Remove predeclared identifiers from unresolved list.
	for n := range unresolved {
		if n == "nil" || predeclaredTypes[n] || predeclaredConstants[n] || predeclaredFuncs[n] {
			delete(unresolved, n)
		}
	}

	// Use unresolved identifiers to determine the imports used by this
	// example. The heuristic assumes package names match base import
	// paths. (Should be good enough most of the time.)
	imports := make(map[string]string) // [name]path
	for _, s := range file.Imports {
		p, err := strconv.Unquote(s.Path.Value)
		if err != nil {
			continue
		}
		n := path.Base(p)
		if s.Name != nil {
			if s.Name.Name == "." {
				// We can't resolve dot imports (yet).
				return nil
			}
			n = s.Name.Name
		}
		if unresolved[n] {
			imports[n] = p
			delete(unresolved, n)
		}
	}

	// If there are other unresolved identifiers, give up because this
	// synthesized file is not going to build.
	if len(unresolved) > 0 {
		return nil
	}

	// Filter out comments that are outside the function body.
	var comments []*ast.CommentGroup
	for _, c := range file.Comments {
		if c.Pos() < body.Pos() || c.Pos() >= body.End() {
			continue
		}
		comments = append(comments, c)
	}

	// Strip "Output:" commment and adjust body end position.
	body, comments = stripOutputComment(body, comments)

	// Synthesize import declaration.
	importDecl := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: 1, // Need non-zero Lparen and Rparen so that printer
		Rparen: 1, // treats this as a factored import.
	}
	for n, p := range imports {
		s := &ast.ImportSpec{Path: &ast.BasicLit{Value: strconv.Quote(p)}}
		if path.Base(p) != n {
			s.Name = ast.NewIdent(n)
		}
		importDecl.Specs = append(importDecl.Specs, s)
	}

	// Synthesize main function.
	funcDecl := &ast.FuncDecl{
		Name: ast.NewIdent("main"),
		Type: &ast.FuncType{},
		Body: body,
	}

	// Synthesize file.
	return &ast.File{
		Name:     ast.NewIdent("main"),
		Decls:    []ast.Decl{importDecl, funcDecl},
		Comments: comments,
	}
}

// playExample takes a whole file example and synthesizes a new *ast.File
// such that the example is function main in package main.
func playExampleFile(file *ast.File) *ast.File {
	// Strip copyright comment if present.
	comments := file.Comments
	if len(comments) > 0 && strings.HasPrefix(comments[0].Text(), "Copyright") {
		comments = comments[1:]
	}

	// Copy declaration slice, rewriting the ExampleX function to main.
	var decls []ast.Decl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && isTest(f.Name.Name, "Example") {
			// Copy the FuncDecl, as it may be used elsewhere.
			newF := *f
			newF.Name = ast.NewIdent("main")
			newF.Body, comments = stripOutputComment(f.Body, comments)
			d = &newF
		}
		decls = append(decls, d)
	}

	// Copy the File, as it may be used elsewhere.
	f := *file
	f.Name = ast.NewIdent("main")
	f.Decls = decls
	f.Comments = comments
	return &f
}

// stripOutputComment finds and removes an "Output:" commment from body 
// and comments, and adjusts the body block's end position.
func stripOutputComment(body *ast.BlockStmt, comments []*ast.CommentGroup) (*ast.BlockStmt, []*ast.CommentGroup) {
	// Do nothing if no "Output:" comment found.
	i, last := lastComment(body, comments)
	if last == nil || !outputPrefix.MatchString(last.Text()) {
		return body, comments
	}

	// Copy body and comments, as the originals may be used elsewhere.
	newBody := &ast.BlockStmt{
		Lbrace: body.Lbrace,
		List:   body.List,
		Rbrace: last.Pos(),
	}
	newComments := make([]*ast.CommentGroup, len(comments)-1)
	copy(newComments, comments[:i])
	copy(newComments[i:], comments[i+1:])
	return newBody, newComments
}

// lastComment returns the last comment inside the provided block.
func lastComment(b *ast.BlockStmt, c []*ast.CommentGroup) (i int, last *ast.CommentGroup) {
	pos, end := b.Pos(), b.End()
	for j, cg := range c {
		if cg.Pos() < pos {
			continue
		}
		if cg.End() > end {
			break
		}
		i, last = j, cg
	}
	return
}
