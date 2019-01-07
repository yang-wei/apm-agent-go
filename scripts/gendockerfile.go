// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/build"
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

var (
	baseFlag = flag.String("base", ".", "base directory of the repo, relative to the working directory")
	outFlag  = flag.String("o", "Dockerfile-testing", "output file, relative to this directory")
	diffFlag = flag.Bool("d", false, "diff file against output file instead of writing")
)

// This generates a Dockerfile that explicitly runs "go get" for each
// external import.
var dockerfileTemplate = template.Must(template.New("Dockerfile").Parse(`
# Code generated by gendockerfile. DO NOT EDIT.
FROM golang:latest
WORKDIR /go/src/go.elastic.co/apm
{{range .Imports}}RUN go get -v {{.}}
{{end}}
ADD . /go/src/go.elastic.co/apm
`[1:]))

// isExternal reports whether or not importPath refers to
// an external import: one outside of the standard library
// or the vendor directory.
func isExternal(importPath string) bool {
	r := strings.IndexRune(importPath, '/')
	if r == -1 {
		return false
	}
	return strings.IndexRune(importPath[:r], '.') != -1
}

func relPath(p string) string {
	if *baseFlag == "." {
		return "./" + p
	}
	return path.Join(*baseFlag, p)
}

func main() {
	flag.Parse()
	cmd := exec.Command("go", "list", "-json", relPath("..."), relPath("vendor/..."))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}

	externalImports := make(map[string]bool)
	decoder := json.NewDecoder(stdout)
	for {
		var pkg build.Package
		if err := decoder.Decode(&pkg); err != nil {
			if err != io.EOF {
				log.Fatal(err)
			}
			break
		}
		externalImports[pkg.ImportPath] = false
		imports := append(pkg.Imports, pkg.TestImports...)
		imports = append(imports, pkg.XTestImports...)
		for _, importPath := range imports {
			if _, ok := externalImports[importPath]; ok {
				continue
			}
			externalImports[importPath] = isExternal(importPath)
		}
	}

	var data struct {
		Imports []string
	}
	for importPath, isExternal := range externalImports {
		if isExternal {
			data.Imports = append(data.Imports, importPath)
		}
	}
	sort.Strings(data.Imports)

	var buf bytes.Buffer
	var out io.Writer = &buf
	outFile := filepath.Join(*baseFlag, "scripts", *outFlag)
	if !*diffFlag {
		f, err := os.Create(outFile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		out = f
	}
	if err := dockerfileTemplate.Execute(out, &data); err != nil {
		log.Fatal(err)
	}
	if *diffFlag {
		cmd := exec.Command("diff", "-c", outFile, "-")
		cmd.Stdin = &buf
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	}
}
